// Copyright (c) 2019 The Gnet Authors. All rights reserved.
// Copyright (c) 2019 Chao yuepan, Allen Xu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE

// Package ring 实现了一个内存高效的环形缓冲区。
package ring

import (
	"errors"
	"io"
	"math/bits"
)

const (
	// MinRead 表示 ReadFrom 每次尝试从 Reader 读取的最小字节数。
	// 只要缓冲区中多出的可写空间大于等于 MinRead，就不会触发扩容。
	MinRead = 512
	// DefaultBufferSize 是环形缓冲区的默认初始大小。
	DefaultBufferSize   = 1024     // 1KB
	bufferGrowThreshold = 4 * 1024 // 4KB
)

// ErrIsEmpty 表示当前环形缓冲区为空，无法继续读取。
var ErrIsEmpty = errors.New("ring-buffer is empty")

// Buffer 是一个环形缓冲区，实现了 io.Reader 和 io.Writer 接口。
type Buffer struct {
	buf     []byte // 底层字节切片
	size    int    // 缓冲区容量（始终为 2 的幂）
	r       int    // 下一次读取位置
	w       int    // 下一次写入位置
	isEmpty bool   // r == w 时用于区分“空/满”状态
}

// New 创建一个给定初始容量的 Buffer。
// size 会被向上取整为 2 的幂；size 为 0 时，仅创建一个逻辑上的空缓冲区。
func New(size int) *Buffer {
	if size == 0 {
		return &Buffer{isEmpty: true}
	}
	size = ceilToPowerOfTwo(size)
	return &Buffer{
		buf:     make([]byte, size),
		size:    size,
		isEmpty: true,
	}
}

// Peek 返回接下来 n 个字节但不会前进读指针。
// 当 n <= 0 时，返回所有可读数据。
// 返回值被拆分为 head/tail 两段，以适配读指针跨越环形边界的情况。
func (rb *Buffer) Peek(n int) (head []byte, tail []byte) {
	if rb.isEmpty {
		return
	}

	if n <= 0 {
		return rb.peekAll()
	}

	if rb.w > rb.r {
		m := rb.w - rb.r // length of ring-buffer
		if m > n {
			m = n
		}
		head = rb.buf[rb.r : rb.r+m]
		return
	}

	m := rb.size - rb.r + rb.w // length of ring-buffer
	if m > n {
		m = n
	}

	if rb.r+m <= rb.size {
		head = rb.buf[rb.r : rb.r+m]
	} else {
		c1 := rb.size - rb.r
		head = rb.buf[rb.r:]
		c2 := m - c1
		tail = rb.buf[:c2]
	}

	return
}

// peekAll 返回所有可读数据，但不会前进读指针。
func (rb *Buffer) peekAll() (head []byte, tail []byte) {
	if rb.isEmpty {
		return
	}

	if rb.w > rb.r {
		head = rb.buf[rb.r:rb.w]
		return
	}

	head = rb.buf[rb.r:]
	if rb.w != 0 {
		tail = rb.buf[:rb.w]
	}

	return
}

// Discard 丢弃接下来 n 个字节，通过移动读指针实现。
// 返回实际丢弃的字节数以及可能的错误。
func (rb *Buffer) Discard(n int) (discarded int, err error) {
	if n <= 0 {
		return 0, nil
	}

	discarded = rb.Buffered()
	if n < discarded {
		rb.r = (rb.r + n) % rb.size
		return n, nil
	}
	rb.Reset()
	return
}

// Read 实现 io.Reader 接口，从环形缓冲区读取数据到 p 中。
//
// 说明：
//   - 返回值 n 表示实际读取的字节数（0 <= n <= len(p)）；
//   - 当缓冲区为空时返回 ErrIsEmpty；
//   - 当可读数据不足 len(p) 时，尽可能多地读取可用数据并立即返回；
//   - 读指针会被相应向前推进，当数据全部读完时，缓冲区会被重置为“空”状态。
func (rb *Buffer) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	if rb.isEmpty {
		return 0, ErrIsEmpty
	}

	if rb.w > rb.r {
		n = rb.w - rb.r
		if n > len(p) {
			n = len(p)
		}
		copy(p, rb.buf[rb.r:rb.r+n])
		rb.r += n
		if rb.r == rb.w {
			rb.Reset()
		}
		return
	}

	n = rb.size - rb.r + rb.w
	if n > len(p) {
		n = len(p)
	}

	if rb.r+n <= rb.size {
		copy(p, rb.buf[rb.r:rb.r+n])
	} else {
		c1 := rb.size - rb.r
		copy(p, rb.buf[rb.r:])
		c2 := n - c1
		copy(p[c1:], rb.buf[:c2])
	}
	rb.r = (rb.r + n) % rb.size
	if rb.r == rb.w {
		rb.Reset()
	}

	return
}

// ReadByte 读取并返回下一个字节，当缓冲区为空时返回 ErrIsEmpty。
func (rb *Buffer) ReadByte() (b byte, err error) {
	if rb.isEmpty {
		return 0, ErrIsEmpty
	}
	b = rb.buf[rb.r]
	rb.r++
	if rb.r == rb.size {
		rb.r = 0
	}
	if rb.r == rb.w {
		rb.Reset()
	}

	return
}

// Write 实现 io.Writer 接口，将 p 中的数据写入环形缓冲区。
//
// 说明：
//   - 返回值 n 为写入的字节数，满足 n == len(p) > 0；
//   - 当剩余可写空间不足时，会自动扩容底层缓冲区；
//   - 不会修改调用方传入的 p 切片内容。
func (rb *Buffer) Write(p []byte) (n int, err error) {
	n = len(p)
	if n == 0 {
		return
	}

	free := rb.Available()
	if n > free {
		rb.grow(rb.size + n - free)
	}

	if rb.w >= rb.r {
		c1 := rb.size - rb.w
		if c1 >= n {
			copy(rb.buf[rb.w:], p)
			rb.w += n
		} else {
			copy(rb.buf[rb.w:], p[:c1])
			c2 := n - c1
			copy(rb.buf, p[c1:])
			rb.w = c2
		}
	} else {
		copy(rb.buf[rb.w:], p)
		rb.w += n
	}

	if rb.w == rb.size {
		rb.w = 0
	}

	rb.isEmpty = false

	return
}

// WriteByte 向缓冲区写入单个字节。
func (rb *Buffer) WriteByte(c byte) error {
	if rb.Available() < 1 {
		rb.grow(1)
	}
	rb.buf[rb.w] = c
	rb.w++

	if rb.w == rb.size {
		rb.w = 0
	}
	rb.isEmpty = false

	return nil
}

// Buffered 返回当前缓冲区中可读数据的字节数。
func (rb *Buffer) Buffered() int {
	if rb.r == rb.w {
		if rb.isEmpty {
			return 0
		}
		return rb.size
	}

	if rb.w > rb.r {
		return rb.w - rb.r
	}

	return rb.size - rb.r + rb.w
}

// Len 返回底层缓冲区的长度（等同于 Cap）。
func (rb *Buffer) Len() int {
	return len(rb.buf)
}

// Cap 返回底层缓冲区的容量。
func (rb *Buffer) Cap() int {
	return rb.size
}

// Available 返回当前缓冲区中可写入的剩余字节数。
func (rb *Buffer) Available() int {
	if rb.r == rb.w {
		if rb.isEmpty {
			return rb.size
		}
		return 0
	}

	if rb.w < rb.r {
		return rb.r - rb.w
	}

	return rb.size - rb.w + rb.r
}

// WriteString 将字符串 s 的内容写入缓冲区。
func (rb *Buffer) WriteString(s string) (int, error) {
	return rb.Write([]byte(s))
}

// Bytes 返回当前所有可读数据的拷贝。
// 不会移动读指针，仅复制内部数据。
func (rb *Buffer) Bytes() []byte {
	if rb.isEmpty {
		return nil
	} else if rb.w == rb.r {
		var bb []byte
		bb = append(bb, rb.buf[rb.r:]...)
		bb = append(bb, rb.buf[:rb.w]...)
		return bb
	}

	var bb []byte
	if rb.w > rb.r {
		bb = append(bb, rb.buf[rb.r:rb.w]...)
		return bb
	}

	bb = append(bb, rb.buf[rb.r:]...)

	if rb.w != 0 {
		bb = append(bb, rb.buf[:rb.w]...)
	}

	return bb
}

// ReadFrom 实现 io.ReaderFrom 接口，从 r 连续读取数据并写入缓冲区。
func (rb *Buffer) ReadFrom(r io.Reader) (n int64, err error) {
	var m int
	for {
		if rb.Available() < MinRead {
			rb.grow(rb.Buffered() + MinRead)
		}

		if rb.w >= rb.r {
			m, err = r.Read(rb.buf[rb.w:])
			if m < 0 {
				panic("RingBuffer.ReadFrom: reader returned negative count from Read")
			}
			rb.isEmpty = false
			rb.w = (rb.w + m) % rb.size
			n += int64(m)
			if err == io.EOF {
				return n, nil
			}
			if err != nil {
				return
			}
			m, err = r.Read(rb.buf[:rb.r])
			if m < 0 {
				panic("RingBuffer.ReadFrom: reader returned negative count from Read")
			}
			rb.w = (rb.w + m) % rb.size
			n += int64(m)
			if err == io.EOF {
				return n, nil
			}
			if err != nil {
				return
			}
		} else {
			m, err = r.Read(rb.buf[rb.w:rb.r])
			if m < 0 {
				panic("RingBuffer.ReadFrom: reader returned negative count from Read")
			}
			rb.isEmpty = false
			rb.w = (rb.w + m) % rb.size
			n += int64(m)
			if err == io.EOF {
				return n, nil
			}
			if err != nil {
				return
			}
		}
	}
}

// WriteTo 实现 io.WriterTo 接口，将当前所有可读数据写入 w。
func (rb *Buffer) WriteTo(w io.Writer) (int64, error) {
	if rb.isEmpty {
		return 0, ErrIsEmpty
	}

	if rb.w > rb.r {
		n := rb.w - rb.r
		m, err := w.Write(rb.buf[rb.r : rb.r+n])
		if m > n {
			panic("RingBuffer.WriteTo: invalid Write count")
		}
		rb.r += m
		if rb.r == rb.w {
			rb.Reset()
		}
		if err != nil {
			return int64(m), err
		}
		if !rb.isEmpty {
			return int64(m), io.ErrShortWrite
		}
		return int64(m), nil
	}

	n := rb.size - rb.r + rb.w
	if rb.r+n <= rb.size {
		m, err := w.Write(rb.buf[rb.r : rb.r+n])
		if m > n {
			panic("RingBuffer.WriteTo: invalid Write count")
		}
		rb.r = (rb.r + m) % rb.size
		if rb.r == rb.w {
			rb.Reset()
		}
		if err != nil {
			return int64(m), err
		}
		if !rb.isEmpty {
			return int64(m), io.ErrShortWrite
		}
		return int64(m), nil
	}

	var cum int64
	c1 := rb.size - rb.r
	m, err := w.Write(rb.buf[rb.r:])
	if m > c1 {
		panic("RingBuffer.WriteTo: invalid Write count")
	}
	rb.r = (rb.r + m) % rb.size
	if err != nil {
		return int64(m), err
	}
	if m < c1 {
		return int64(m), io.ErrShortWrite
	}
	cum += int64(m)
	c2 := n - c1
	m, err = w.Write(rb.buf[:c2])
	if m > c2 {
		panic("RingBuffer.WriteTo: invalid Write count")
	}
	rb.r = m
	cum += int64(m)
	if rb.r == rb.w {
		rb.Reset()
	}
	if err != nil {
		return cum, err
	}
	if !rb.isEmpty {
		return cum, io.ErrShortWrite
	}
	return cum, nil
}

// IsFull 返回当前环形缓冲区是否已满。
func (rb *Buffer) IsFull() bool {
	return rb.r == rb.w && !rb.isEmpty
}

// IsEmpty 返回当前环形缓冲区是否为空。
func (rb *Buffer) IsEmpty() bool {
	return rb.isEmpty
}

// Reset 将读写指针重置为 0，并将缓冲区标记为“空”状态。
func (rb *Buffer) Reset() {
	rb.isEmpty = true
	rb.r, rb.w = 0, 0
}

func (rb *Buffer) grow(newCap int) {
	if n := rb.size; n == 0 {
		if newCap <= DefaultBufferSize {
			newCap = DefaultBufferSize
		} else {
			newCap = ceilToPowerOfTwo(newCap)
		}
	} else {
		doubleCap := n + n
		if newCap <= doubleCap {
			if n < bufferGrowThreshold {
				newCap = doubleCap
			} else {
				// Check 0 < n to detect overflow and prevent an infinite loop.
				for 0 < n && n < newCap {
					n += n / 4
				}
				// The n calculation doesn't overflow, set n to newCap.
				if n > 0 {
					newCap = n
				}
			}
		}
	}
	newBuf := make([]byte, newCap)
	oldLen := rb.Buffered()
	_, _ = rb.Read(newBuf)
	rb.buf = newBuf
	rb.r = 0
	rb.w = oldLen
	rb.size = newCap
	if rb.w > 0 {
		rb.isEmpty = false
	}
}

// ceilToPowerOfTwo 将 n 向上取整为最接近的 2 的幂。
// 若 n 已经是 2 的幂，则直接返回 n。
func ceilToPowerOfTwo(n int) int {
	if n <= 0 {
		return 0
	}
	// n 已经是 2 的幂。
	if n&(n-1) == 0 {
		return n
	}
	return 1 << bits.Len(uint(n))
}

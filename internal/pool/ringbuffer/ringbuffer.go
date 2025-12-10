// Copyright (c) 2019 The Gnet Authors. All rights reserved.
// Copyright (c) 2016 Aliaksandr Valialkin, VertaMedia
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Use of this source code is governed by a MIT license that can be found
// at https://github.com/valyala/bytebufferpool/blob/master/LICENSE

// Package ringbuffer 实现了一个适用于环形缓冲区的对象池，用于降低 GC 压力。
package ringbuffer

import (
	"math/bits"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/lk2023060901/danmu-garden-go/pkg/buffer/ring"
)

const (
	minBitSize = 6 // 2**6=64，为典型 CPU cache line 大小
	steps      = 20

	minSize = 1 << minBitSize

	calibrateCallsThreshold = 42000
	maxPercentile           = 0.95
)

// RingBuffer 是 ring.Buffer 的别名，便于在池中引用。
type RingBuffer = ring.Buffer

// Pool 表示环形缓冲区的对象池。
//
// 说明：
//   - 不同用途可以使用不同的 Pool，以减少内存浪费；
//   - 内部会根据使用频率自动校准默认大小和最大可回收大小。
type Pool struct {
	calls       [steps]uint64
	calibrating uint64

	defaultSize uint64
	maxSize     uint64

	pool sync.Pool
}

var builtinPool Pool

// Get 从默认池中获取一个空的环形缓冲区。
//
// 通过 Put 归还缓冲区可以显著减少内存分配次数。
func Get() *RingBuffer { return builtinPool.Get() }

// Get 从指定 Pool 中获取一个新的环形缓冲区。
//
// 返回的缓冲区长度为 0，可在使用结束后通过 Put 归还，以降低 GC 开销。
func (p *Pool) Get() *RingBuffer {
	v := p.pool.Get()
	if v != nil {
		return v.(*RingBuffer)
	}
	return ring.New(int(atomic.LoadUint64(&p.defaultSize)))
}

// Put 将环形缓冲区归还到默认池中。
//
// 注意：归还后的 RingBuffer 不允许再被访问，否则会引发数据竞争。
func Put(b *RingBuffer) { builtinPool.Put(b) }

// Put 将通过 Get 获取的缓冲区归还到 Pool 中。
//
// 注意：归还后的缓冲区不允许再被访问。
func (p *Pool) Put(b *RingBuffer) {
	idx := index(b.Len())

	if atomic.AddUint64(&p.calls[idx], 1) > calibrateCallsThreshold {
		p.calibrate()
	}

	maxSize := int(atomic.LoadUint64(&p.maxSize))
	if maxSize == 0 || b.Cap() <= maxSize {
		b.Reset()
		p.pool.Put(b)
	}
}

func (p *Pool) calibrate() {
	if !atomic.CompareAndSwapUint64(&p.calibrating, 0, 1) {
		return
	}

	a := make(callSizes, 0, steps)
	var callsSum uint64
	for i := uint64(0); i < steps; i++ {
		calls := atomic.SwapUint64(&p.calls[i], 0)
		callsSum += calls
		a = append(a, callSize{
			calls: calls,
			size:  minSize << i,
		})
	}
	sort.Sort(a)

	defaultSize := a[0].size
	maxSize := defaultSize

	maxSum := uint64(float64(callsSum) * maxPercentile)
	callsSum = 0
	for i := 0; i < steps; i++ {
		if callsSum > maxSum {
			break
		}
		callsSum += a[i].calls
		size := a[i].size
		if size > maxSize {
			maxSize = size
		}
	}

	atomic.StoreUint64(&p.defaultSize, defaultSize)
	atomic.StoreUint64(&p.maxSize, maxSize)

	atomic.StoreUint64(&p.calibrating, 0)
}

type callSize struct {
	calls uint64
	size  uint64
}

type callSizes []callSize

func (ci callSizes) Len() int {
	return len(ci)
}

func (ci callSizes) Less(i, j int) bool {
	return ci[i].calls > ci[j].calls
}

func (ci callSizes) Swap(i, j int) {
	ci[i], ci[j] = ci[j], ci[i]
}

func index(n int) int {
	n--
	n >>= minBitSize
	idx := 0
	if n > 0 {
		idx = bits.Len(uint(n))
	}
	if idx >= steps {
		idx = steps - 1
	}
	return idx
}

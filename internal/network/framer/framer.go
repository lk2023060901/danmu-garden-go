package framer

import (
	"encoding/binary"
	"fmt"
	"io"

	"google.golang.org/protobuf/proto"

	"github.com/lk2023060901/danmu-garden-go/internal/pool/bytebuffer"
	protos "github.com/lk2023060901/danmu-garden-framework-protos/framework"
)

// Framer 抽象了基于 Envelope 的打包/解包能力。
//
// 约定：
//   - 一帧数据的格式为：4 字节大端无符号整型（表示后续 Envelope 序列化后的长度）+ Envelope 二进制数据。
//   - Envelope 的序列化与反序列化由 protobuf 负责。
type Framer interface {
	// WriteFrame 将 Envelope 打包为一帧并写入到 w 中。
	WriteFrame(w io.Writer, env *protos.Envelope) error

	// ReadFrame 从 r 中读取一帧数据并解包为 Envelope。
	ReadFrame(r io.Reader) (*protos.Envelope, error)
}

// LengthPrefixedFramer 使用长度前缀（4 字节大端）作为帧边界。
// 适用于基于流的连接（如 TCP、WebSocket 原始流等）。
type LengthPrefixedFramer struct {
	// MaxFrameSize 为允许的最大帧大小（Envelope 序列化后长度），单位字节。
	// 为 0 时使用默认值 defaultMaxFrameSize。
	MaxFrameSize uint32
}

const defaultMaxFrameSize uint32 = 16 * 1024 * 1024 // 16MB

// NewLengthPrefixedFramer 创建一个长度前缀帧编码器。
// maxFrameSize 为 0 时使用默认值。
func NewLengthPrefixedFramer(maxFrameSize uint32) *LengthPrefixedFramer {
	if maxFrameSize == 0 {
		maxFrameSize = defaultMaxFrameSize
	}
	return &LengthPrefixedFramer{
		MaxFrameSize: maxFrameSize,
	}
}

// WriteFrame 将 Envelope 编码为长度前缀帧并写入。
func (f *LengthPrefixedFramer) WriteFrame(w io.Writer, env *protos.Envelope) error {
	if env == nil {
		return fmt.Errorf("framer: envelope is nil")
	}

	// 自动修正 size 字段，保证与 payload 长度一致。
	if env.Payload != nil {
		env.Size = uint32(len(env.Payload))
	} else {
		env.Size = 0
	}

	body, err := proto.Marshal(env)
	if err != nil {
		return fmt.Errorf("framer: marshal envelope failed: %w", err)
	}

	length := uint32(len(body))
	if length > f.effectiveMaxSize() {
		return fmt.Errorf("framer: frame size %d exceeds max %d", length, f.effectiveMaxSize())
	}

	var header [4]byte
	binary.BigEndian.PutUint32(header[:], length)

	if _, err := w.Write(header[:]); err != nil {
		return fmt.Errorf("framer: write header failed: %w", err)
	}
	if length == 0 {
		return nil
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("framer: write body failed: %w", err)
	}

	return nil
}

// ReadFrame 从流中读取一帧数据并解码为 Envelope。
func (f *LengthPrefixedFramer) ReadFrame(r io.Reader) (*protos.Envelope, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, fmt.Errorf("framer: read header failed: %w", err)
	}

	length := binary.BigEndian.Uint32(header[:])
	if length > f.effectiveMaxSize() {
		return nil, fmt.Errorf("framer: frame size %d exceeds max %d", length, f.effectiveMaxSize())
	}

	// 使用 ByteBuffer 池降低频繁 make 带来的分配与 GC 压力。
	buf := bytebuffer.Get()
	defer bytebuffer.Put(buf)

	if length > 0 {
		// 确保底层切片容量足够。
		if cap(buf.B) < int(length) {
			buf.B = make([]byte, int(length))
		} else {
			buf.B = buf.B[:int(length)]
		}

		if _, err := io.ReadFull(r, buf.B); err != nil {
			return nil, fmt.Errorf("framer: read body failed: %w", err)
		}
	}

	env := &protos.Envelope{}
	if length == 0 {
		// 空帧视为空 Envelope。
		return env, nil
	}

	if err := proto.Unmarshal(buf.B[:length], env); err != nil {
		return nil, fmt.Errorf("framer: unmarshal envelope failed: %w", err)
	}
	return env, nil
}

func (f *LengthPrefixedFramer) effectiveMaxSize() uint32 {
	if f == nil || f.MaxFrameSize == 0 {
		return defaultMaxFrameSize
	}
	return f.MaxFrameSize
}

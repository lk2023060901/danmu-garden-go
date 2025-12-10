package framer

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/lk2023060901/danmu-garden-go/internal/pool/bytebuffer"
	protos "github.com/lk2023060901/danmu-garden-framework-protos/framework"
)

// Framer 抽象了基于 Envelope 的打包/解包能力。
//
// 约定：
//   - 一帧数据的格式为：固定长度的 MessageHeader 头部（二进制结构，不包含 payload 字段）+ 真正的 payload 字节流。
//   - 头部中 size 字段表示 payload 的真实字节长度。
//   - 不再额外添加 4 字节长度前缀。
type Framer interface {
	// WriteFrame 将 Envelope 打包为一帧并写入到 w 中。
	WriteFrame(w io.Writer, env *protos.Envelope) error

	// ReadFrame 从 r 中读取一帧数据并解包为 Envelope。
	ReadFrame(r io.Reader) (*protos.Envelope, error)
}

// LengthPrefixedFramer 使用固定大小的头部作为帧边界。
// 适用于基于流的连接（如 TCP、WebSocket 原始流等）。
type LengthPrefixedFramer struct {
	// MaxFrameSize 为允许的最大帧大小（Envelope 序列化后长度），单位字节。
	// 为 0 时使用默认值 defaultMaxFrameSize。
	MaxFrameSize uint32
}

const defaultMaxFrameSize uint32 = 16 * 1024 * 1024 // 16MB
const envelopeHeaderSize = 32                       // op(4) + seq(8) + size(4) + flags(8) + timestamp(8)

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

	// 确保 Header 存在。
	if env.Header == nil {
		env.Header = &protos.MessageHeader{}
	}

	// 自动修正 size 字段，保证与 payload 长度一致。
	var payload []byte
	if env.Payload != nil {
		payload = env.Payload
		if len(payload) > int(f.effectiveMaxSize()) {
			return fmt.Errorf("framer: payload size %d exceeds max %d", len(payload), f.effectiveMaxSize())
		}
		env.Header.Size = uint32(len(payload))
	} else {
		env.Header.Size = 0
		payload = nil
	}

	// 构造固定长度的头部（不包含 payload）。
	var header [envelopeHeaderSize]byte
	binary.BigEndian.PutUint32(header[0:4], env.Header.Op)
	binary.BigEndian.PutUint64(header[4:12], env.Header.Seq)
	binary.BigEndian.PutUint32(header[12:16], env.Header.Size)
	binary.BigEndian.PutUint64(header[16:24], env.Header.Flags)
	binary.BigEndian.PutUint64(header[24:32], uint64(env.Header.Timestamp))

	if _, err := w.Write(header[:]); err != nil {
		return fmt.Errorf("framer: write header failed: %w", err)
	}
	if len(payload) == 0 {
		return nil
	}
	n, err := w.Write(payload)
	if err != nil {
		return fmt.Errorf("framer: write payload failed: %w", err)
	}
	if n != len(payload) {
		return fmt.Errorf("framer: write payload short write: %d < %d", n, len(payload))
	}

	return nil
}

// ReadFrame 从流中读取一帧数据并解码为 Envelope。
//
// Wire 格式：
//   - 固定 32 字节头部（不含 payload），字段顺序为：
//     op(uint32) | seq(uint64) | size(uint32) | flags(uint64) | timestamp(int64)
//   - 后续紧跟 size 字节的 payload 原始数据。
func (f *LengthPrefixedFramer) ReadFrame(r io.Reader) (*protos.Envelope, error) {
	var header [envelopeHeaderSize]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, fmt.Errorf("framer: read header failed: %w", err)
	}

	env := &protos.Envelope{
		Header: &protos.MessageHeader{},
	}
	env.Header.Op = binary.BigEndian.Uint32(header[0:4])
	env.Header.Seq = binary.BigEndian.Uint64(header[4:12])
	env.Header.Size = binary.BigEndian.Uint32(header[12:16])
	env.Header.Flags = binary.BigEndian.Uint64(header[16:24])
	env.Header.Timestamp = int64(binary.BigEndian.Uint64(header[24:32]))

	if env.Header.Size > f.effectiveMaxSize() {
		return nil, fmt.Errorf("framer: payload size %d exceeds max %d", env.Header.Size, f.effectiveMaxSize())
	}

	// 使用 ByteBuffer 池降低频繁 make 带来的分配与 GC 压力。
	size := int(env.Header.Size)
	if size == 0 {
		// 无 payload，直接返回头部信息。
		return env, nil
	}

	buf := bytebuffer.Get()
	defer bytebuffer.Put(buf)

	// 确保底层切片容量足够。
	if cap(buf.B) < size {
		buf.B = make([]byte, size)
	} else {
		buf.B = buf.B[:size]
	}

	if _, err := io.ReadFull(r, buf.B); err != nil {
		return nil, fmt.Errorf("framer: read payload failed: %w", err)
	}

	// 将 payload 拷贝到 Envelope，避免与 ByteBuffer 池复用产生数据篡改。
	env.Payload = make([]byte, size)
	copy(env.Payload, buf.B)

	return env, nil
}

func (f *LengthPrefixedFramer) effectiveMaxSize() uint32 {
	if f.MaxFrameSize == 0 {
		return defaultMaxFrameSize
	}
	return f.MaxFrameSize
}

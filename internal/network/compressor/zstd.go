package compressor

import (
	"github.com/klauspost/compress/zstd"

	"github.com/lk2023060901/danmu-garden-go/pkg/util/hardware"
)

// ZstdCompressor 基于 github.com/klauspost/compress/zstd 的压缩实现。
//
// 它持有独立的 encoder/decoder 实例：
//   - 不使用全局单例，避免不同调用方之间的隐式耦合。
//   - 由框架或调用方自行决定实例的生命周期与复用策略。
type ZstdCompressor struct {
	enc            *zstd.Encoder
	dec            *zstd.Decoder
	minCompressSize int
}

// 编译期断言：确保 ZstdCompressor 实现了 Compressor 接口。
var _ Compressor = (*ZstdCompressor)(nil)

// NewZstdCompressor 创建一个 ZstdCompressor，默认并发度为主机 CPU 核心数。
func NewZstdCompressor() (*ZstdCompressor, error) {
	return NewZstdCompressorWithConcurrency(0)
}

// NewZstdCompressorWithConcurrency 创建一个 ZstdCompressor，并允许显式指定 zstd 的并发数。
//
// 参数说明：
//   - concurrency <= 0：使用主机 CPU 核心数（hardware.GetCPUNum()）。
//   - concurrency > 0 ：使用指定并发度。
func NewZstdCompressorWithConcurrency(concurrency int) (*ZstdCompressor, error) {
	if concurrency <= 0 {
		concurrency = hardware.GetCPUNum()
	}

	opts := []zstd.EOption{
		zstd.WithZeroFrames(true),
		zstd.WithEncoderConcurrency(concurrency),
	}

	enc, err := zstd.NewWriter(nil, opts...)
	if err != nil {
		return nil, err
	}
	dec, err := zstd.NewReader(nil)
	if err != nil {
		enc.Close()
		return nil, err
	}
	return &ZstdCompressor{
		enc: enc,
		dec: dec,
	}, nil
}

// SetMinCompressSize 设置触发压缩的最小字节数。
// 当 src 长度小于该值时，Compress 直接返回原始数据，不做压缩。
func (c *ZstdCompressor) SetMinCompressSize(n int) {
	if n < 0 {
		n = 0
	}
	c.minCompressSize = n
}

// Compress 实现 Compressor 接口。
func (c *ZstdCompressor) Compress(dst, src []byte) ([]byte, error) {
	if c == nil || c.enc == nil {
		return nil, zstd.ErrEncoderClosed
	}

	// 小于阈值时不压缩，直接返回原始数据。
	if c.minCompressSize > 0 && len(src) < c.minCompressSize {
		return src, nil
	}

	out := c.enc.EncodeAll(src, dst[:0])
	return out, nil
}

// Decompress 实现 Compressor 接口。
func (c *ZstdCompressor) Decompress(dst, src []byte) ([]byte, error) {
	if c == nil || c.dec == nil {
		return nil, zstd.ErrDecoderClosed
	}
	out, err := c.dec.DecodeAll(src, dst[:0])
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Close 释放内部 encoder/decoder 持有的资源。
//
// 调用方可在不再需要该压缩器时显式关闭；再次使用已关闭实例将返回 ErrEncoderClosed/ErrDecoderClosed。
func (c *ZstdCompressor) Close() {
	if c == nil {
		return
	}
	if c.enc != nil {
		_ = c.enc.Close()
		c.enc = nil
	}
	if c.dec != nil {
		c.dec.Close()
		c.dec = nil
	}
}

package compressor

type Compressor interface {
	// Compress 将 src 压缩后返回新的字节切片。
	Compress(src []byte) (packet []byte, err error)

	// Decompress 将压缩数据 src 解压后返回新的字节切片。
	//
	// 行为约定与 Compress 对称：src 必须是 Compress 的输出。
	Decompress(src []byte) (plain []byte, err error)
}

// NopCompressor 是一个空实现：不做任何压缩/解压，直接返回输入内容。
//
// 适用于：
//   - 框架默认值（未开启压缩功能时）
//   - 便于在调用侧通过接口注入，在不改业务逻辑的前提下关闭压缩
type NopCompressor struct{}

func (NopCompressor) Compress(src []byte) ([]byte, error) {
	return src, nil
}

func (NopCompressor) Decompress(src []byte) ([]byte, error) {
	return src, nil
}

// 编译期断言：确保 NopCompressor 实现了 Compressor 接口。
var _ Compressor = NopCompressor{}

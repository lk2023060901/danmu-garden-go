package compressor

// Compressor 抽象了“单次压缩/解压”能力。
//
// 设计目标：
//   - 面向网络消息或内存块压缩，而不是文件/Parquet 之类的复杂场景。
//   - 不做全局单例，调用方按需创建具体实现的实例。
type Compressor interface {
	// Compress 将 src 压缩到 dst。
	//
	// dst 一般可以传入一个可复用的缓冲区（长度可为 0），实现可选择复用其底层容量；
	// 返回值 packet 为压缩后的完整数据。
	Compress(dst, src []byte) (packet []byte, err error)

	// Decompress 将压缩数据 src 解压到 dst。
	//
	// 行为约定与 Compress 对称：src 必须是 Compress 的输出。
	Decompress(dst, src []byte) (plain []byte, err error)
}

// NopCompressor 是一个空实现：不做任何压缩/解压，直接返回输入内容。
//
// 适用于：
//   - 框架默认值（未开启压缩功能时）
//   - 便于在调用侧通过接口注入，在不改业务逻辑的前提下关闭压缩
type NopCompressor struct{}

func (NopCompressor) Compress(_ []byte, src []byte) ([]byte, error) {
	return src, nil
}

func (NopCompressor) Decompress(_ []byte, src []byte) ([]byte, error) {
	return src, nil
}

// 编译期断言：确保 NopCompressor 实现了 Compressor 接口。
var _ Compressor = NopCompressor{}

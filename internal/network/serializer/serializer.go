package serializer

// Serializer 抽象了网络层“对象 <-> 字节流”的序列化能力。
//
// 设计目标：
//   - 面向网络消息编码，既支持 JSON，也支持 Protobuf 等二进制格式。
//   - 调用方通过接口注入具体实现，便于后续扩展其它序列化方案。
type Serializer interface {
	// Marshal 将任意对象编码为字节序列。
	Marshal(v any) ([]byte, error)

	// Unmarshal 将字节序列解码到目标对象。
	//
	// v 通常为指针类型，用于接收解码结果。
	Unmarshal(data []byte, v any) error
}

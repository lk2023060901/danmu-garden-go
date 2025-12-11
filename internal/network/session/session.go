package session

import (
	"context"
	"net"
	"sync/atomic"

	protos "github.com/lk2023060901/danmu-garden-game-protos"
)

// Packet 直接使用已有的 Envelope 作为网络层数据包。
type Packet = protos.Envelope

// MessageHeader 是 Envelope 的头部别名，便于在依赖方引用而无需直接依赖 proto 包。
type MessageHeader = protos.MessageHeader

// IDConstraint 限定 Session ID 的可选类型。
//
// 当前支持 uint64 或 string，两者足以覆盖大部分会话标识需求。
type IDConstraint interface {
	~uint64 | ~string
}

// IDGenerator 用于生成会话 ID。
//
// 通过泛型支持 uint64 或 string 等类型，具体类型由调用方在实例化时决定。
type IDGenerator[ID IDConstraint] interface {
	Next() ID
}

// Uint64IDGenerator 是一个简单的基于原子自增序列的 ID 生成器。
type Uint64IDGenerator struct {
	seq atomic.Uint64
}

// Next 返回下一个 uint64 ID。
func (g *Uint64IDGenerator) Next() uint64 {
	return g.seq.Add(1)
}

// ServerSession 抽象了服务器侧的一条网络会话。
//
// 说明：
//   - ID 类型由泛型参数 ID 决定（通常为 uint64 或 string）；
//   - 仅关心通用的会话属性与发送/接收能力，不绑定具体传输协议。
type ServerSession[ID IDConstraint] interface {
	// ID 返回该会话的唯一标识。
	ID() ID

	// Context 返回会话关联的上下文。
	Context() context.Context

	// RemoteAddr 返回远端地址。
	RemoteAddr() net.Addr

	// LocalAddr 返回本端地址。
	LocalAddr() net.Addr

	// Send 发送一个高层业务对象。
	//
	// 具体编码逻辑（业务对象 -> Packet/字节）由底层 Codec 决定。
	Send(op uint32, msg any) error

	// Recv 返回只读的 Packet 接收通道。
	//
	// 说明：
	//   - 主要用于需要主动拉取消息的场景；
	//   - 不强制要求所有调用方都使用该通道，Handler 也可以在 decode 后直接处理。
	Recv() <-chan *Packet

	// Close 主动关闭会话。
	Close() error

	// OnDisconnected 在底层连接关闭或出现不可恢复错误时被调用一次。
	//
	// 实现可用于做内部清理或触发上层通知。
	OnDisconnected(err error)
}

// Session 是使用 uint64 作为 ID 类型的服务器侧默认会话抽象。
//
// 说明：为了兼容现有代码（例如 router.Router），保留该别名作为非泛型入口。
type Session = ServerSession[uint64]

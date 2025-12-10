package session

import (
	"context"
	"net"
)

// Session 抽象了一条网络会话/连接。
//
// 约定：
//   - 每个 Session 对应一条底层连接（例如一个 TCP 连接或 WebSocket 会话）。
//   - Session ID 使用 64 位无符号整型，在框架内应保持全局唯一。
//   - 框架层只关心会话本身，不关心“玩家”等具体业务概念。
type Session interface {
	// ID 返回该会话在框架内的全局唯一标识。
	//
	// 说明：
	//   - 一般由框架在接入连接时分配（例如自增 uint64）。
	//   - 业务层可以通过该 ID 建立 “Session <-> 玩家/用户” 的映射关系。
	ID() uint64

	// Context 返回与该会话关联的上下文。
	//
	// 说明：
	//   - 可用于级联取消：当会话关闭时，应触发 Context.Done()。
	//   - 业务层可使用 Context 存放少量元数据（不建议存放大对象）。
	Context() context.Context

	// RemoteAddr 返回远端地址（客户端地址）。
	//
	// 说明：
	//   - 对于 TCP 连接，通常为 "ip:port"。
	//   - 主要用于日志记录、审计或限流策略。
	RemoteAddr() net.Addr

	// LocalAddr 返回本端地址（服务器监听地址）。
	//
	// 说明：
	//   - 在多监听端口或多网卡场景下，可用于区分不同入口。
	LocalAddr() net.Addr

	// Send 通过框架的编解码链路向该会话发送一条业务消息。
	//
	// 参数：
	//   - op  ：协议号，由调用方指定；
	//   - msg ：业务层的请求/响应对象，由内部 Codec/Serializer 负责序列化与封包。
	//
	// 行为：
	//   - 内部会按照 Codec 的 pipeline 依次执行：序列化 -> 压缩 -> 加密 -> 封装 Envelope -> 写出帧。
	//   - 调用方只关心发送语义，不需要直接操作 Envelope 或底层连接。
	Send(op uint32, msg any) error

	// Close 主动关闭该会话。
	//
	// 说明：
	//   - 应关闭底层连接，并触发 Context 的取消。
	//   - 多次调用应是幂等的：对已关闭的会话再次调用 Close 不应引发 panic。
	Close() error

	// OnConnected 在会话（底层连接）建立成功后被调用一次。
	//
	// 说明：
	//   - 由接入层在完成 OnAccept 并创建好 Session 后调用；
	//   - 可在实现中执行：初始化玩家上下文、打日志、注册到业务映射表等。
	OnConnected()

	// OnDisconnected 在会话检测到底层连接断开时被调用。
	//
	// 参数：
	//   - err 为断开原因；正常关闭时可为 nil。
	//
	// 说明：
	//   - 由接入层在读写错误、心跳超时或主动关闭 Session 时调用；
	//   - 用于让 Session 自身感知断线，做内部清理或触发重连逻辑（如果有）。
	OnDisconnected(err error)
}

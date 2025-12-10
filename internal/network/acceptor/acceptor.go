package acceptor

import (
	"context"
	"net"

	"github.com/lk2023060901/danmu-garden-go/internal/network/codec"
	"github.com/lk2023060901/danmu-garden-go/internal/network/session"
	protos "github.com/lk2023060901/danmu-garden-framework-protos/framework"
)

// Handler 由框架使用者实现，用于在接入层的各个阶段插入自定义逻辑。
//
// 设计目标：
//   - OnAccept：决定“一个新连接如何封装为 Session”；
//   - OnMessage：统一处理已解码的消息（通常内部会调用 router.Handle）；
//   - OnSessionClosed：在会话生命周期结束时做业务级清理；
//   - OnError：统一记录和处理接入层过程中出现的错误。
type Handler interface {
	// OnAccept 在 accept 到一个新连接后被调用，用于创建自定义 Session 实例。
	//
	// 参数：
	//   - ctx  ：该会话的上下文，Acceptor 会在会话整个生命周期内保持有效，
	//            当会话关闭时 ctx 会被取消；
	//   - conn ：底层网络连接（例如 *net.TCPConn），调用方可在自定义 Session 中持有；
	//   - c    ：为该连接准备好的 Codec，可选持有用于主动发送消息等。
	//
	// 返回：
	//   - sess ：用户自定义的 Session 实例（需实现 Session 接口，包括 OnConnected/OnDisconnected）；
	//   - err  ：出错时，Acceptor 会关闭 conn，不进入后续处理流程。
	OnAccept(ctx context.Context, conn net.Conn, c codec.Codec) (sess session.Session, err error)

	// OnMessage 在成功读取并解码一条消息后被调用。
	//
	// 参数：
	//   - sess   ：当前会话；
	//   - header ：对端发来的消息头（包含 op/seq/flags/timestamp 等元数据）；
	//   - payload：已完成解密/解压的业务层字节序列。
	//
	// 典型实现：
	//   - 直接调用 router.Handle(sess, header, payload) 做协议路由；
	//   - 或在调用前后增加统计、链路追踪等通用逻辑。
	OnMessage(sess session.Session, header *protos.MessageHeader, payload []byte)

	// OnSessionClosed 在会话生命周期结束时被调用。
	//
	// 参数：
	//   - sess：被关闭的会话；
	//   - err ：关闭原因（正常关闭可为 nil）。
	//
	// 典型用途：
	//   - 从 SessionManager 或业务映射表中移除；
	//   - 做上线/下线统计与日志记录。
	OnSessionClosed(sess session.Session, err error)

	// OnError 在接入层的读写、解码或业务处理过程中发生错误时被调用。
	//
	// 说明：
	//   - 可用于统一记录错误、打监控、决定是否立即关闭会话等。
	OnError(sess session.Session, err error)

	// OnTimeout 在 Accept 或会话读写发生超时时被调用。
	//
	// 参数：
	//   - sess：当超时发生在 Accept 阶段时为 nil；发生在已有会话上时为对应的 Session；
	//
	// 返回值：
	//   - err ：当返回非 nil 时，接入器会结束当前流程并将该错误向上返回；
	//           返回 nil 表示忽略本次超时（Accept 继续监听 / Session 继续收消息）。
	//
	// 典型用途：
	//   - 根据具体业务策略，决定是否断开连接、记录慢连接日志或进行限流等。
	OnTimeout(sess session.Session) error
}

// Acceptor 抽象了服务器侧的“接入层”。
//
// 职责：
//   - 在指定地址上监听并接受新连接；
//   - 为每个连接调用 Handler.OnAccept 创建 Session，并调用 sess.OnConnected()；
//   - 为每个 Session 启动收包循环，使用 Codec 解码后回调 Handler.OnMessage；
//   - 在会话结束时调用 sess.OnDisconnected(err) 与 Handler.OnSessionClosed。
type Acceptor interface {
	// Serve 启动接入循环，在各个阶段回调 Handler。
	//
	// 行为约定：
	//   - ctx 取消时，应停止接收新连接，并逐步关闭已有会话，最终返回；
	//   - 返回值为导致退出的错误（正常退出时可为 ctx.Err() 或 nil）。
	Serve(ctx context.Context, h Handler) error

	// Close 主动关闭监听和所有会话。
	//
	// 说明：
	//   - 应关闭底层监听器，并对所有已创建的 Session 调用 Close；
	//   - 多次调用应是幂等的。
	Close() error
}

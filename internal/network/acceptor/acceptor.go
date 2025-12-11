package acceptor

import (
	"context"
	"net"
	"time"

	"github.com/gorilla/websocket"

	network "github.com/lk2023060901/danmu-garden-go/internal/network"
	"github.com/lk2023060901/danmu-garden-go/internal/network/codec"
	"github.com/lk2023060901/danmu-garden-go/internal/network/session"
)

// 为方便引用，直接复用 session 包中的 Packet 定义。
type Packet = session.Packet

// Config 描述 Acceptor 在会话层面的配置。
//
// 说明：
//   - SendQueueSize/RecvQueueSize 控制每个连接的发送/接收缓冲队列大小；
//   - ReadTimeout/WriteTimeout 控制单次读写的超时时间（为 0 表示不设置 deadline）；
//   - Path 控制 WebSocket 的升级路径（如 "/ws"）。
type Config struct {
	SendQueueSize int
	RecvQueueSize int

	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	Path string

	// Upgrader 允许调用方自定义 gorilla/websocket 的升级行为。
	// 若为 nil，则使用内部默认的 Upgrader。
	Upgrader *websocket.Upgrader

	// Codec 为当前接入层使用的编解码器。
	// 用于在 op+msg <-> Envelope(二进制帧) 之间转换。
	Codec codec.Codec
}

// 默认配置。
func defaultConfig() Config {
	return Config{
		SendQueueSize: 1024,
		RecvQueueSize: 1024,
		Path:          "/ws",
	}
}

// AcceptorHandler 由框架使用者实现，用于在服务器侧的各个阶段插入自定义逻辑。
//
// 说明：
//   - ID 为 Session ID 的具体类型（例如 uint64 或 string）；
//   - 所有回调均在单个会话的收/发协程中被调用，应避免耗时操作阻塞网络收发。
type AcceptorHandler[ID session.IDConstraint] interface {
	// OnConnected 在握手成功并创建好会话后被调用。
	OnConnected(sess session.ServerSession[ID])

	// OnMessage 在成功解码出一条消息后被调用。
	//
	// header 为消息头（包含 op/seq/flags 等），payload 为业务明文字节。
	OnMessage(sess session.ServerSession[ID], header *session.MessageHeader, payload []byte)

	// OnClosed 在会话生命周期结束时被调用。
	//
	// 参数 err 为关闭原因，正常关闭时可为 nil。
	OnClosed(sess session.ServerSession[ID], err error)

	// OnError 在会话处理的各个阶段发生错误时被调用。
	//
	// stage 用于标识错误发生的位置，便于监控与排查。
	OnError(sess session.ServerSession[ID], stage network.Stage, err error)
}

// Acceptor 抽象了服务器侧的 WebSocket 接入层。
//
// 职责：
//   - 在指定 listener 上监听 HTTP，并处理 WebSocket 升级；
//   - 为每个连接创建 ServerSession，并调用 AcceptorHandler 的各阶段回调；
//   - 维护当前活跃会话列表，便于运维与监控。
type Acceptor[ID session.IDConstraint] interface {
	// Serve 在给定 listener 上启动服务，阻塞直至 ctx 取消或出现致命错误。
	Serve(ctx context.Context, ln net.Listener, h AcceptorHandler[ID]) error

	// Close 主动关闭所有会话以及内部资源。
	Close() error

	// Sessions 返回当前活跃会话的快照。
	Sessions() []session.ServerSession[ID]
}

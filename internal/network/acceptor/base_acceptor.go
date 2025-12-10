package acceptor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/lk2023060901/danmu-garden-go/internal/network/codec"
	"github.com/lk2023060901/danmu-garden-go/internal/network/session"
	protos "github.com/lk2023060901/danmu-garden-framework-protos/framework"
)

// BaseAcceptor 是 Acceptor 接口的基础 TCP 实现。
//
// 设计目标：
//   - 对外只暴露 Acceptor 接口和 Handler 回调，不绑定具体业务逻辑；
//   - 内部负责：监听端口、接受连接、创建 Session、驱动解码并回调 Handler；
//   - 每个连接使用独立的 goroutine 串行处理消息，保证同一 Session 上 Handler 串行执行。
type BaseAcceptor struct {
	ln       net.Listener
	codec    codec.Codec
	sessions session.SessionManager

	closeOnce sync.Once
}

// 确保 BaseAcceptor 实现了 Acceptor 接口。
var _ Acceptor = (*BaseAcceptor)(nil)

// inboundFrame 表示一条已解码但尚未交由业务处理的消息帧。
// header 为 MessageHeader，payload 为已完成解密/解压缩的业务字节。
type inboundFrame struct {
	header  *protos.MessageHeader
	payload []byte
}

const defaultInboundQueueSize = 1024

// NewBaseAcceptor 使用已有的 Listener 创建一个基础接入器。
//
// 参数：
//   - ln ：已创建好的 net.Listener（例如 TCP 监听器）；
//   - c  ：用于当前接入器所有连接的 Codec；
//   - sm ：SessionManager，可为 nil；非 nil 时会在连接建立/关闭时自动注册和移除 Session。
func NewBaseAcceptor(ln net.Listener, c codec.Codec, sm session.SessionManager) (*BaseAcceptor, error) {
	if ln == nil {
		return nil, fmt.Errorf("acceptor: listener is nil")
	}
	if c == nil {
		return nil, fmt.Errorf("acceptor: codec is nil")
	}
	return &BaseAcceptor{
		ln:       ln,
		codec:    c,
		sessions: sm,
	}, nil
}

// NewTCPAcceptor 在给定地址上监听 TCP，并创建一个基础接入器。
//
// 参数：
//   - addr：监听地址，例如 "0.0.0.0:9000"；
//   - c   ：用于当前接入器所有连接的 Codec；
//   - sm  ：SessionManager，可为 nil。
func NewTCPAcceptor(addr string, c codec.Codec, sm session.SessionManager) (*BaseAcceptor, error) {
	if addr == "" {
		return nil, fmt.Errorf("acceptor: addr is empty")
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	return NewBaseAcceptor(ln, c, sm)
}

// Serve 实现 Acceptor.Serve。
func (a *BaseAcceptor) Serve(ctx context.Context, h Handler) error {
	if h == nil {
		return fmt.Errorf("acceptor: handler is nil")
	}
	if a.ln == nil {
		return fmt.Errorf("acceptor: listener is nil")
	}

	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		conn, err := a.ln.Accept()
		if err != nil {
			// 若上层已取消，则将错误视为正常退出。
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			// 若为超时错误，则交由 Handler.OnTimeout 决定后续行为。
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				if terr := h.OnTimeout(nil); terr != nil {
					// OnTimeout 返回非 nil，结束接入循环，将错误向上返回。
					return terr
				}
				// 忽略本次超时，继续接受新连接。
				continue
			}

			// 其他错误交由上层决定是否重试或重建接入器。
			return err
		}

		wg.Add(1)
		go func(conn net.Conn) {
			defer wg.Done()
			a.handleConnection(ctx, conn, h)
		}(conn)
	}
}

// Close 实现 Acceptor.Close。
func (a *BaseAcceptor) Close() error {
	var err error
	a.closeOnce.Do(func() {
		if a.ln != nil {
			err = a.ln.Close()
		}
	})
	return err
}

// handleConnection 处理单个连接的生命周期。
//
// 流程：
//   1. 调用 Handler.OnAccept 创建 Session 实例；
//   2. 可选地将 Session 注册到 SessionManager；
//   3. 调用 sess.OnConnected()；
//   4. 通过读协程循环读取并解码消息帧，将结果投递到 per-session 消息队列；
//   5. 在当前协程中按顺序从队列中取出消息，并回调 Handler.OnMessage；
//   6. 读写或解码失败后，调用 sess.OnDisconnected(err) 与 Handler.OnSessionClosed。
func (a *BaseAcceptor) handleConnection(parentCtx context.Context, conn net.Conn, h Handler) {
	// 为该会话创建独立的上下文，便于级联取消。
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	// 创建 Session。
	sess, err := h.OnAccept(ctx, conn, a.codec)
	if err != nil {
		_ = conn.Close()
		h.OnError(nil, err)
		return
	}
	if sess == nil {
		_ = conn.Close()
		return
	}

	// 注册 Session（可选）。
	if a.sessions != nil {
		if err := a.sessions.Register(sess); err != nil {
			h.OnError(sess, err)
			_ = sess.Close()
			return
		}
		defer func() {
			_ = a.sessions.Unregister(sess.ID())
		}()
	}

	// 通知会话已建立。
	sess.OnConnected()

	// 在函数结束时负责通知断开和关闭。
	var cause error
	defer func() {
		sess.OnDisconnected(cause)
		h.OnSessionClosed(sess, cause)
		_ = sess.Close()
	}()

	// per-session 消息队列：读协程负责投递，当前协程顺序消费。
	frames := make(chan inboundFrame, defaultInboundQueueSize)

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		cause = a.readLoop(ctx, sess, conn, h, frames)
		close(frames)
	}()

	// 顺序消费消息帧，确保同一 Session 上的业务 Handler 串行执行。
	for frame := range frames {
		h.OnMessage(sess, frame.header, frame.payload)
	}

	// 等待读协程退出。
	wg.Wait()

	// 若没有显式错误原因，则使用上下文错误（例如超时或被取消）。
	if cause == nil && ctx.Err() != nil && !errors.Is(ctx.Err(), context.Canceled) {
		cause = ctx.Err()
	}
}

// readLoop 持续从连接中读取并解码消息帧，将结果写入 frames 通道。
//
// 返回值：
//   - 非 nil error 表示读/解码过程中发生的错误（包括 OnTimeout 返回的错误）；
//   - nil 表示正常结束（例如对端关闭连接）。
func (a *BaseAcceptor) readLoop(ctx context.Context, sess session.Session, conn net.Conn, h Handler, frames chan<- inboundFrame) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		header, payload, err := a.codec.DecodeRaw(conn)
		if err != nil {
			// EOF/连接关闭视为正常断开。
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return nil
			}

			// 超时错误交由 OnTimeout 决定是否结束会话。
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				if terr := h.OnTimeout(sess); terr != nil {
					return terr
				}
				// 忽略本次超时，继续读取。
				continue
			}

			// 其他错误交由 OnError 处理，并结束会话。
			h.OnError(sess, err)
			return err
		}

		// 将完整帧投递到 per-session 消息队列。
		select {
		case frames <- inboundFrame{header: header, payload: payload}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

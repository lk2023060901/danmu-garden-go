package session

import (
	"context"
	"net"
	"sync"

	protos "github.com/lk2023060901/danmu-garden-framework-protos/framework"
	"github.com/lk2023060901/danmu-garden-go/internal/network/codec"
	"github.com/lk2023060901/danmu-garden-go/internal/pool/ringbuffer"
)

// BaseSession 提供了 Session 接口的基础实现。
//
// 设计目标：
//   - 封装最小但完整的会话能力：ID、Context、地址信息、发送与关闭；
//   - 默认实现 OnConnected/OnDisconnected 为空，方便业务在自定义 Session 中嵌入并覆写。
type BaseSession struct {
	id uint64

	ctx    context.Context
	cancel context.CancelFunc

	conn  net.Conn
	codec codec.Codec

	remoteAddr net.Addr
	localAddr  net.Addr

	// recvBuf 为当前会话的接收缓冲区（字节级环形队列）。
	//   - 用于存放尚未完全解包的字节流；
	//   - 典型场景：一次 Read 收到多条或半条消息，剩余字节写入 recvBuf，等待下次继续拼包。
	recvBuf *ringbuffer.RingBuffer

	// sendBuf 为当前会话的发送缓冲区（字节级环形队列）。
	//   - 用于暂存待发送的完整帧字节，统一从该缓冲区刷到底层连接；
	//   - 可以看作“发送队列”的底层实现，后续可配合独立发送协程或 netpoll 使用。
	sendBuf *ringbuffer.RingBuffer

	// sendQueue 为待发送消息的对象级队列。
	//   - Send 仅负责将 (header, msg) 投递到该队列；
	//   - 独立的发送协程从队列中取出消息，编码到 sendBuf 并刷到底层连接。
	sendQueue chan outboundMessage

	// seq 为当前会话维护的本地自增序号。
	//   - 仅用于由服务器侧主动发送的消息；
	//   - 客户端请求中的 seq 通常由对端维护。
	seq uint64

	closeOnce sync.Once
}

// 确保 BaseSession 实现了 Session 接口。
var _ Session = (*BaseSession)(nil)

// outboundMessage 表示一条待发送的协议消息。
type outboundMessage struct {
	op  uint32
	msg any
}

// defaultSendQueueSize 为每个会话的发送队列容量。
const defaultSendQueueSize = 1024

// NewBaseSession 创建一个基于 net.Conn 的基础 Session 实例。
//
// 参数：
//   - parent：会话所属的上层上下文（例如 Acceptor 的 Serve ctx）；若为 nil，则使用 context.Background()；
//   - id    ：会话 ID，应在框架或调用侧保证全局唯一；
//   - conn  ：底层网络连接；
//   - c     ：用于该连接的 Codec。
func NewBaseSession(parent context.Context, id uint64, conn net.Conn, c codec.Codec) *BaseSession {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)

	s := &BaseSession{
		id:         id,
		ctx:        ctx,
		cancel:     cancel,
		conn:       conn,
		codec:      c,
		remoteAddr: conn.RemoteAddr(),
		localAddr:  conn.LocalAddr(),
		recvBuf:    ringbuffer.Get(),
		sendBuf:    ringbuffer.Get(),
	}

	// 初始化发送队列及发送协程。
	s.sendQueue = make(chan outboundMessage, defaultSendQueueSize)
	go s.sendLoop()

	return s
}

// ID 实现 Session.ID。
func (s *BaseSession) ID() uint64 {
	return s.id
}

// Context 实现 Session.Context。
func (s *BaseSession) Context() context.Context {
	return s.ctx
}

// RemoteAddr 实现 Session.RemoteAddr。
func (s *BaseSession) RemoteAddr() net.Addr {
	return s.remoteAddr
}

// LocalAddr 实现 Session.LocalAddr。
func (s *BaseSession) LocalAddr() net.Addr {
	return s.localAddr
}

// Send 实现 Session.Send。
//
// 内部仅将消息投递到会话级发送队列，由独立的发送协程按顺序构造 MessageHeader、
// 调用 Codec 进行编码并写入到底层连接。这样可以避免多 goroutine 并发写 conn 导致的报文交叉。
func (s *BaseSession) Send(op uint32, msg any) error {
	select {
	case <-s.ctx.Done():
		// 会话已关闭或上层已取消。
		return s.ctx.Err()
	case s.sendQueue <- outboundMessage{op: op, msg: msg}:
		return nil
	}
}

// Close 实现 Session.Close。
func (s *BaseSession) Close() error {
	var err error
	s.closeOnce.Do(func() {
		// 先取消上下文，再关闭连接。
		if s.cancel != nil {
			s.cancel()
		}
		if s.conn != nil {
			err = s.conn.Close()
		}

		// 归还环形缓冲区到对象池。
		if s.recvBuf != nil {
			ringbuffer.Put(s.recvBuf)
			s.recvBuf = nil
		}

		if s.sendQueue != nil {
			close(s.sendQueue)
			s.sendQueue = nil
		}

		if s.sendBuf != nil {
			ringbuffer.Put(s.sendBuf)
			s.sendBuf = nil
		}
	})
	return err
}

// OnConnected 默认实现为空，方便在自定义 Session 中覆写。
func (s *BaseSession) OnConnected() {}

// OnDisconnected 默认实现为空，方便在自定义 Session 中覆写。
func (s *BaseSession) OnDisconnected(error) {}

// sendLoop 为每个会话启动的专职发送协程。
//
// 行为：
//   - 从 sendQueue 中按顺序取出待发送消息；
//   - 若配置了 sendBuf，则先编码到 sendBuf，再从中分批写入 conn；
//   - 若未配置 sendBuf，则直接编码到 conn。
func (s *BaseSession) sendLoop() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case msg, ok := <-s.sendQueue:
			if !ok {
				return
			}

			// 发送路径仅在此协程中执行，避免多协程并发写 conn。
			if s.sendBuf == nil {
				header := s.buildHeader(msg.op)
				_ = s.codec.Encode(s.conn, header, msg.msg)
				continue
			}

			// 将本条消息编码到发送缓冲区。
			header := s.buildHeader(msg.op)
			if err := s.codec.Encode(s.sendBuf, header, msg.msg); err != nil {
				// 编码失败视为会话异常，取消上下文以触发上层清理。
				if s.cancel != nil {
					s.cancel()
				}
				return
			}

			// 将发送缓冲区中的数据尽可能刷到底层连接。
			if err := s.flushSendBuf(); err != nil {
				if s.cancel != nil {
					s.cancel()
				}
				return
			}
		}
	}
}

// flushSendBuf 将 sendBuf 中的所有字节尽可能写入到底层连接。
//
// 说明：
//   - 使用固定大小的临时缓冲区分批写出；
//   - 对单次 Write 的短写情况进行显式处理，直到当前块完全写出。
func (s *BaseSession) flushSendBuf() error {
	if s.conn == nil {
		return nil
	}

	var tmp [4096]byte

	for s.sendBuf.Buffered() > 0 {
		n, _ := s.sendBuf.Read(tmp[:])
		if n <= 0 {
			break
		}

		written := 0
		for written < n {
			m, err := s.conn.Write(tmp[written:n])
			if err != nil {
				return err
			}
			if m <= 0 {
				return nil
			}
			written += m
		}
	}

	return nil
}

// buildHeader 根据协议号构造一条待发送消息的 MessageHeader。
//
// 说明：
//   - 当前实现仅填充 Op 字段，并维护一个简单的自增 seq；
//   - timestamp、flags 等字段可根据需要在后续演进中补充。
func (s *BaseSession) buildHeader(op uint32) *protos.MessageHeader {
	s.seq++
	return &protos.MessageHeader{
		Op:  op,
		Seq: s.seq,
	}
}

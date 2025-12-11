package acceptor

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	network "github.com/lk2023060901/danmu-garden-go/internal/network"
	"github.com/lk2023060901/danmu-garden-go/internal/network/codec"
	"github.com/lk2023060901/danmu-garden-go/internal/network/session"
	"github.com/lk2023060901/danmu-garden-go/pkg/util/conc"
)

// wsAcceptor 是 Acceptor 的基于 gorilla/websocket 的默认实现。
type wsAcceptor[ID session.IDConstraint] struct {
	cfg   Config
	idGen session.IDGenerator[ID]

	upgrader websocket.Upgrader

	mu       sync.RWMutex
	sessions map[ID]*wsSession[ID]
}

// NewWSAcceptor 创建一个基于 WebSocket 的 Acceptor 实例。
//
// 参数：
//   - cfg   ：基础配置，留空字段将使用默认值；
//   - idGen ：会话 ID 生成器，若为 nil 且 ID 为 uint64，则使用默认的 Uint64IDGenerator。
func NewWSAcceptor[ID session.IDConstraint](cfg Config, idGen session.IDGenerator[ID]) Acceptor[ID] {
	def := defaultConfig()
	if cfg.SendQueueSize <= 0 {
		cfg.SendQueueSize = def.SendQueueSize
	}
	if cfg.RecvQueueSize <= 0 {
		cfg.RecvQueueSize = def.RecvQueueSize
	}
	if cfg.Path == "" {
		cfg.Path = def.Path
	}

	if cfg.Codec == nil {
		panic("acceptor: Codec is nil")
	}

	var upgrader websocket.Upgrader
	if cfg.Upgrader != nil {
		upgrader = *cfg.Upgrader
	} else {
		upgrader = websocket.Upgrader{
			// 读写缓冲大小交由 gorilla 默认处理即可。
			CheckOrigin: func(r *http.Request) bool { return true },
		}
	}

	// 若调用方未提供 ID 生成器且 ID 为 uint64，则使用默认实现。
	if idGen == nil {
		var zero ID
		switch any(zero).(type) {
		case uint64:
			idGen = any(&session.Uint64IDGenerator{}).(session.IDGenerator[ID])
		}
	}

	return &wsAcceptor[ID]{
		cfg:      cfg,
		idGen:    idGen,
		upgrader: upgrader,
		sessions: make(map[ID]*wsSession[ID]),
	}
}

// Serve 在给定 listener 上启动 HTTP+WebSocket 服务。
func (a *wsAcceptor[ID]) Serve(ctx context.Context, ln net.Listener, h AcceptorHandler[ID]) error {
	mux := http.NewServeMux()
	mux.HandleFunc(a.cfg.Path, func(w http.ResponseWriter, r *http.Request) {
		conn, err := a.upgrader.Upgrade(w, r, nil)
		if err != nil {
			// 握手失败直接返回，由上层日志记录。
			return
		}

		sessID := a.idGen.Next()
		sessCtx, cancel := context.WithCancel(ctx)

		ws := newWSSession(sessCtx, cancel, sessID, conn, a.cfg, h)

		a.mu.Lock()
		a.sessions[sessID] = ws
		a.mu.Unlock()

		h.OnConnected(ws)
	})

	srv := &http.Server{
		Handler: mux,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		if err := srv.Shutdown(context.Background()); err != nil && !errors.Is(err, http.ErrServerClosed) {
			// 无法直接关联到具体会话，这里仅返回 ctx.Err。
		}
		// 关闭所有会话，忽略关闭过程中的错误。
		a.Close()
		return ctx.Err()
	case err := <-errCh:
		// 尝试关闭所有会话，将 Close 的错误优先级低于 srv.Serve 返回的错误。
		if closeErr := a.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		return err
	}
}

// Close 关闭所有会话。
func (a *wsAcceptor[ID]) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, s := range a.sessions {
		// 会话关闭失败在各自的 close 中上报，这里忽略返回值。
		s.Close()
	}
	a.sessions = make(map[ID]*wsSession[ID])
	return nil
}

// Sessions 返回当前会话快照。
func (a *wsAcceptor[ID]) Sessions() []session.ServerSession[ID] {
	a.mu.RLock()
	defer a.mu.RUnlock()

	out := make([]session.ServerSession[ID], 0, len(a.sessions))
	for _, s := range a.sessions {
		out = append(out, s)
	}
	return out
}

// outboundMessage 表示一条待发送的业务消息。
type outboundMessage struct {
	op  uint32
	msg any
}

// wsSession 是基于 WebSocket 的 ServerSession 默认实现。
type wsSession[ID session.IDConstraint] struct {
	id ID

	conn *websocket.Conn

	ctx    context.Context
	cancel context.CancelFunc

	cfg Config

	handler AcceptorHandler[ID]

	remoteAddr net.Addr
	localAddr  net.Addr

	sendChan chan outboundMessage
	recvChan chan *Packet

	codec codec.Codec

	closeOnce sync.Once
}

// newWSSession 创建一个新的 wsSession，并启动收发协程。
func newWSSession[ID session.IDConstraint](
	ctx context.Context,
	cancel context.CancelFunc,
	id ID,
	conn *websocket.Conn,
	cfg Config,
	h AcceptorHandler[ID],
) *wsSession[ID] {
	s := &wsSession[ID]{
		id:        id,
		conn:      conn,
		ctx:       ctx,
		cancel:    cancel,
		cfg:       cfg,
		handler:    h,
		remoteAddr: conn.RemoteAddr(),
		localAddr:  conn.LocalAddr(),
		sendChan:   make(chan outboundMessage, cfg.SendQueueSize),
		recvChan:   make(chan *Packet, cfg.RecvQueueSize),
		codec:      cfg.Codec,
	}

	// 使用封装的 conc.Go 启动收发协程，避免直接使用原生 go 关键字。
	_ = conc.Go(func() (struct{}, error) {
		s.recvLoop()
		return struct{}{}, nil
	})
	_ = conc.Go(func() (struct{}, error) {
		s.sendLoop()
		return struct{}{}, nil
	})

	return s
}

// 编译期断言：wsSession 实现了 ServerSession 接口（以 uint64 为例）。
var _ session.ServerSession[uint64] = (*wsSession[uint64])(nil)

// ServerSession 接口实现。

func (s *wsSession[ID]) ID() ID                         { return s.id }
func (s *wsSession[ID]) Context() context.Context       { return s.ctx }
func (s *wsSession[ID]) RemoteAddr() net.Addr           { return s.remoteAddr }
func (s *wsSession[ID]) LocalAddr() net.Addr            { return s.localAddr }
func (s *wsSession[ID]) Recv() <-chan *Packet           { return s.recvChan }
func (s *wsSession[ID]) OnDisconnected(error)           {}
func (s *wsSession[ID]) Close() error                   { return s.close(nil) }
func (s *wsSession[ID]) Send(op uint32, msg any) error  { return s.enqueueOutbound(op, msg) }

// 内部辅助方法。

func (s *wsSession[ID]) enqueueOutbound(op uint32, msg any) error {
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	case s.sendChan <- outboundMessage{op: op, msg: msg}:
		return nil
	}
}

func (s *wsSession[ID]) writeRaw(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if s.cfg.WriteTimeout > 0 {
		if err := s.conn.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout)); err != nil {
			s.handler.OnError(s, network.StageSend, err)
			s.close(network.ErrSendFailed)
			return network.ErrSendFailed
		}
	}
	if err := s.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		s.handler.OnError(s, network.StageSend, err)
		s.close(err)
		return network.ErrSendFailed
	}
	return nil
}

func (s *wsSession[ID]) close(cause error) error {
	var err error
	s.closeOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		if s.conn != nil {
			if cerr := s.conn.Close(); cerr != nil && err == nil {
				err = cerr
			}
		}
		close(s.sendChan)
		close(s.recvChan)
		s.handler.OnClosed(s, cause)
		s.OnDisconnected(cause)
	})
	return err
}

// recvLoop 持续从 WebSocket 读取消息并使用 Codec 解码为 Packet。
func (s *wsSession[ID]) recvLoop() {
	defer s.close(nil)

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		if s.cfg.ReadTimeout > 0 {
			if err := s.conn.SetReadDeadline(time.Now().Add(s.cfg.ReadTimeout)); err != nil {
				s.handler.OnError(s, network.StageRecvRaw, err)
				s.close(network.ErrRecvFailed)
				return
			}
		}

		msgType, data, err := s.conn.ReadMessage()
		if err != nil {
			s.handler.OnError(s, network.StageRecvRaw, err)
			s.close(network.ErrRecvFailed)
			return
		}
		if msgType != websocket.BinaryMessage {
			// 当前仅处理二进制帧，其余类型直接忽略。
			continue
		}

		header, payload, err := s.codec.DecodeRaw(bytes.NewReader(data))
		if err != nil {
			s.handler.OnError(s, network.StageDecode, err)
			continue
		}
		if header == nil {
			continue
		}

		pkt := &Packet{
			Header:  header,
			Payload: payload,
		}

		// 投递给接收队列（可选）
		select {
		case <-s.ctx.Done():
			return
		case s.recvChan <- pkt:
		default:
			// 队列已满时丢弃，具体策略可按需调整。
		}

		// 回调业务处理。
		s.handler.OnMessage(s, header, payload)
	}
}

// sendLoop 从 sendChan 读取业务消息并使用 Codec 编码后写入到底层连接。
func (s *wsSession[ID]) sendLoop() {
	defer s.close(nil)

	var seq uint64

	for {
		select {
		case <-s.ctx.Done():
			return
		case msg, ok := <-s.sendChan:
			if !ok {
				return
			}

			seq++
			header := &session.MessageHeader{
				Op:        msg.op,
				Seq:       seq,
				Timestamp: time.Now().UnixNano(),
			}

			var buf bytes.Buffer
			if err := s.codec.Encode(&buf, header, msg.msg); err != nil {
				s.handler.OnError(s, network.StageEncode, err)
				continue
			}

			if err := s.writeRaw(buf.Bytes()); err != nil {
				return
			}
		}
	}
}

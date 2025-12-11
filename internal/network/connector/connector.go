package connector

import (
	"bytes"
	"context"
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

// Packet 与服务器侧保持一致，直接使用 Envelope。
type Packet = session.Packet

// Config 描述客户端连接的基础配置。
type Config struct {
	SendQueueSize int
	RecvQueueSize int

	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	// Codec 为当前连接使用的编解码器。
	// 用于在 op+msg <-> Envelope(二进制帧) 之间转换。
	Codec codec.Codec
}

func defaultConfig() Config {
	return Config{
		SendQueueSize: 1024,
		RecvQueueSize: 1024,
	}
}

// ClientConn 抽象了客户端侧的一条连接。
//
// 注意：客户端连接不包含会话 ID 概念。
type ClientConn interface {
	Context() context.Context
	RemoteAddr() net.Addr
	LocalAddr() net.Addr

	Send(op uint32, msg any) error
	Recv() <-chan *Packet

	Close() error
	OnDisconnected(err error)
}

// ConnectorHandler 描述客户端在各阶段的回调能力。
type ConnectorHandler interface {
	OnConnected(conn ClientConn)
	OnMessage(conn ClientConn, header *session.MessageHeader, payload []byte)
	OnClosed(conn ClientConn, err error)
	OnError(conn ClientConn, stage network.Stage, err error)
}

// Connector 抽象了客户端的拨号器。
type Connector interface {
	Dial(ctx context.Context, urlStr string, h ConnectorHandler, header http.Header) (ClientConn, error)
}

// wsConnector 是基于 gorilla/websocket 的默认 Connector 实现。
type wsConnector struct {
	cfg Config
}

// NewWSConnector 创建一个基于 WebSocket 的 Connector。
func NewWSConnector(cfg Config) Connector {
	def := defaultConfig()
	if cfg.SendQueueSize <= 0 {
		cfg.SendQueueSize = def.SendQueueSize
	}
	if cfg.RecvQueueSize <= 0 {
		cfg.RecvQueueSize = def.RecvQueueSize
	}
	if cfg.Codec == nil {
		panic("connector: Codec is nil")
	}
	return &wsConnector{cfg: cfg}
}

func (c *wsConnector) Dial(ctx context.Context, urlStr string, h ConnectorHandler, header http.Header) (ClientConn, error) {
	dialer := websocket.DefaultDialer

	type result struct {
		conn *websocket.Conn
		err  error
	}
	resCh := make(chan result, 1)

	// 使用 conc.Go 封装 goroutine，避免直接使用 go 关键字。
	_ = conc.Go(func() (struct{}, error) {
		conn, _, err := dialer.Dial(urlStr, header)
		resCh <- result{conn: conn, err: err}
		return struct{}{}, nil
	})

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-resCh:
		if res.err != nil {
			return nil, res.err
		}

		connCtx, cancel := context.WithCancel(ctx)
		cc := newWSClientConn(connCtx, cancel, res.conn, c.cfg, h)
		h.OnConnected(cc)
		return cc, nil
	}
}

// wsClientConn 是基于 WebSocket 的 ClientConn 默认实现。
type wsClientConn struct {
	conn *websocket.Conn

	ctx    context.Context
	cancel context.CancelFunc

	cfg Config
	h   ConnectorHandler

	remoteAddr net.Addr
	localAddr  net.Addr

	sendChan chan outboundMessage
	recvChan chan *Packet

	codec codec.Codec

	closeOnce sync.Once
}

// outboundMessage 表示一条待发送的业务消息。
type outboundMessage struct {
	op  uint32
	msg any
}

func newWSClientConn(
	ctx context.Context,
	cancel context.CancelFunc,
	conn *websocket.Conn,
	cfg Config,
	h ConnectorHandler,
) *wsClientConn {
	c := &wsClientConn{
		conn:       conn,
		ctx:        ctx,
		cancel:     cancel,
		cfg:        cfg,
		h:          h,
		remoteAddr: conn.RemoteAddr(),
		localAddr:  conn.LocalAddr(),
		sendChan:   make(chan outboundMessage, cfg.SendQueueSize),
		recvChan:   make(chan *Packet, cfg.RecvQueueSize),
		codec:      cfg.Codec,
	}

	// 使用 conc.Go 启动收发协程，避免直接使用原生 go 关键字。
	_ = conc.Go(func() (struct{}, error) {
		c.recvLoop()
		return struct{}{}, nil
	})
	_ = conc.Go(func() (struct{}, error) {
		c.sendLoop()
		return struct{}{}, nil
	})

	return c
}

// ClientConn 接口实现。

func (c *wsClientConn) Context() context.Context  { return c.ctx }
func (c *wsClientConn) RemoteAddr() net.Addr      { return c.remoteAddr }
func (c *wsClientConn) LocalAddr() net.Addr       { return c.localAddr }
func (c *wsClientConn) Recv() <-chan *Packet      { return c.recvChan }
func (c *wsClientConn) OnDisconnected(error)      {}
func (c *wsClientConn) Close() error              { return c.close(nil) }

func (c *wsClientConn) Send(op uint32, msg any) error {
	return c.enqueueOutbound(op, msg)
}

func (c *wsClientConn) enqueueOutbound(op uint32, msg any) error {
	select {
	case <-c.ctx.Done():
		return c.ctx.Err()
	case c.sendChan <- outboundMessage{op: op, msg: msg}:
		return nil
	}
}

func (c *wsClientConn) writeRaw(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if c.cfg.WriteTimeout > 0 {
		if err := c.conn.SetWriteDeadline(time.Now().Add(c.cfg.WriteTimeout)); err != nil {
			c.h.OnError(c, network.StageSend, err)
			c.close(network.ErrSendFailed)
			return network.ErrSendFailed
		}
	}
	if err := c.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		c.h.OnError(c, network.StageSend, err)
		c.close(err)
		return network.ErrSendFailed
	}
	return nil
}

func (c *wsClientConn) close(cause error) error {
	var err error
	c.closeOnce.Do(func() {
		if c.cancel != nil {
			c.cancel()
		}
		if c.conn != nil {
			if cerr := c.conn.Close(); cerr != nil && err == nil {
				err = cerr
			}
		}
		close(c.sendChan)
		close(c.recvChan)
		c.h.OnClosed(c, cause)
		c.OnDisconnected(cause)
	})
	return err
}

// recvLoop 持续读取 WebSocket 消息并解码为 Packet。
func (c *wsClientConn) recvLoop() {
	defer c.close(nil)

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		if c.cfg.ReadTimeout > 0 {
			if err := c.conn.SetReadDeadline(time.Now().Add(c.cfg.ReadTimeout)); err != nil {
				c.h.OnError(c, network.StageRecvRaw, err)
				c.close(network.ErrRecvFailed)
				return
			}
		}

		msgType, data, err := c.conn.ReadMessage()
		if err != nil {
			c.h.OnError(c, network.StageRecvRaw, err)
			c.close(network.ErrRecvFailed)
			return
		}
		if msgType != websocket.BinaryMessage {
			continue
		}

		header, payload, err := c.codec.DecodeRaw(bytes.NewReader(data))
		if err != nil {
			c.h.OnError(c, network.StageDecode, err)
			continue
		}
		if header == nil {
			continue
		}

		pkt := &Packet{
			Header:  header,
			Payload: payload,
		}

		select {
		case <-c.ctx.Done():
			return
		case c.recvChan <- pkt:
		default:
		}

		c.h.OnMessage(c, header, payload)
	}
}

// sendLoop 从 sendChan 读取业务消息并使用 Codec 编码后写入 WebSocket。
func (c *wsClientConn) sendLoop() {
	defer c.close(nil)

	for {
		select {
		case <-c.ctx.Done():
			return
		case msg, ok := <-c.sendChan:
			if !ok {
				return
			}

			// 客户端侧通常不维护 seq，若有需要可在上层构造。
			header := &session.MessageHeader{
				Op:        msg.op,
				Timestamp: time.Now().UnixNano(),
			}

			var buf bytes.Buffer
			if err := c.codec.Encode(&buf, header, msg.msg); err != nil {
				c.h.OnError(c, network.StageEncode, err)
				continue
			}

			if err := c.writeRaw(buf.Bytes()); err != nil {
				return
			}
		}
	}
}

package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/lk2023060901/danmu-garden-go/application"
	pingpong "github.com/lk2023060901/danmu-garden-go/examples/network/acceptor-connector/proto"
	networkerr "github.com/lk2023060901/danmu-garden-go/internal/network"
	"github.com/lk2023060901/danmu-garden-go/internal/network/codec"
	"github.com/lk2023060901/danmu-garden-go/internal/network/compressor"
	"github.com/lk2023060901/danmu-garden-go/internal/network/connector"
	"github.com/lk2023060901/danmu-garden-go/internal/network/framer"
	"github.com/lk2023060901/danmu-garden-go/internal/network/serializer"
	"github.com/lk2023060901/danmu-garden-go/internal/network/session"
	"github.com/lk2023060901/danmu-garden-go/pkg/util/conc"
)

const (
	opPing uint32 = 1
	opPong uint32 = 2
)

type reconnectConfig struct {
	baseDelay time.Duration
	maxDelay  time.Duration
}

func (c reconnectConfig) nextDelay(attempt int) time.Duration {
	d := c.baseDelay * time.Duration(1<<attempt)
	if d > c.maxDelay {
		d = c.maxDelay
	}
	return d
}

// clientHandler 实现 ConnectorHandler，负责打印 send/recv，以及错误信息。
type clientHandler struct{}

func (h *clientHandler) OnConnected(conn connector.ClientConn) {
	fmt.Printf("[client] connected: remote=%v local=%v\n", conn.RemoteAddr(), conn.LocalAddr())

	// 每隔 3 秒发送一次 Ping。
	conc.Go(func() (struct{}, error) {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-conn.Context().Done():
				return struct{}{}, nil
			case <-ticker.C:
				req := &pingpong.Ping{Msg: "ping"}
				fmt.Printf("[client] send ping to server: %q\n", req.GetMsg())
				if err := conn.Send(opPing, req); err != nil {
					fmt.Printf("[client] send ping failed: %v\n", err)
				}
			}
		}
	})
}

func (h *clientHandler) OnMessage(conn connector.ClientConn, header *session.MessageHeader, payload []byte) {
	switch header.Op {
	case opPong:
		var resp pingpong.Pong
		var ser serializer.ProtoSerializer
		if err := ser.Unmarshal(payload, &resp); err != nil {
			fmt.Printf("[client] failed to unmarshal Pong: %v\n", err)
			return
		}
		fmt.Printf("[client] recv pong from server: %q\n", resp.GetMsg())
	default:
		fmt.Printf("[client] unknown op=%d\n", header.Op)
	}
}

func (h *clientHandler) OnClosed(conn connector.ClientConn, err error) {
	fmt.Printf("[client] closed: remote=%v err=%v\n", conn.RemoteAddr(), err)
}

func (h *clientHandler) OnError(conn connector.ClientConn, stage networkerr.Stage, err error) {
	if conn != nil {
		fmt.Printf("[client] error: remote=%v stage=%s err=%v\n", conn.RemoteAddr(), stage, err)
	} else {
		fmt.Printf("[client] error: remote=<nil> stage=%s err=%v\n", stage, err)
	}
}

func buildCodec() codec.Codec {
	c, err := codec.New(codec.Options{
		Framer:     framer.NewLengthPrefixedFramer(0),
		Serializer: serializer.ProtoSerializer{},
		Compressor: compressor.NopCompressor{},
	})
	if err != nil {
		log.Fatalf("build codec failed: %v", err)
	}
	return c
}

// manageConnection 负责首连 + 断线重连逻辑。
func manageConnection(ctx context.Context, url string, cfg connector.Config, h connector.ConnectorHandler, rc reconnectConfig) {
	connector := connector.NewWSConnector(cfg)

	conc.Go(func() (struct{}, error) {
		var attempt int

		for {
			select {
			case <-ctx.Done():
				return struct{}{}, ctx.Err()
			default:
			}

			conn, err := connector.Dial(ctx, url, h, http.Header{})
			if err != nil {
				// 首次连接或重连失败，给 handler 一个机会。
				h.OnError(nil, networkerr.StageHandshake, err)

				delay := rc.nextDelay(attempt)
				fmt.Printf("[client] connect failed, will retry in %v (attempt=%d)\n", delay, attempt)
				time.Sleep(delay)
				attempt++
				continue
			}

			// 连接成功，重置重试次数。
			attempt = 0

			// 等待当前连接结束（远端断开 / 本地 Close）。
			<-conn.Context().Done()

			// 这里不区分关闭原因，一律按重连策略重试。
			delay := rc.nextDelay(attempt)
			fmt.Printf("[client] disconnected, will reconnect in %v (attempt=%d)\n", delay, attempt)
			time.Sleep(delay)
			attempt++
		}
	})
}

func main() {
	app := application.New()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 使用 Application 的信号抽象，Ctrl+C 时退出。
	app.OnSignal(func(kind application.SignalKind, sig os.Signal) {
		if kind == application.SignalShutdown {
			fmt.Println("[client] shutdown signal received")
			cancel()
		}
	})

	url := "ws://127.0.0.1:29090/ws"

	connCfg := connector.Config{
		Codec:         buildCodec(),
		SendQueueSize: 1024,
		RecvQueueSize: 1024,
	}
	rcfg := reconnectConfig{
		baseDelay: 500 * time.Millisecond,
		maxDelay:  5 * time.Second,
	}

	h := &clientHandler{}

	// 启动连接管理（首连 + 断线重连）。
	manageConnection(ctx, url, connCfg, h, rcfg)

	// 交由 Application 统一等待和处理退出信号。
	if err := app.Run(); err != nil && err != context.Canceled {
		log.Fatalf("[client] application run failed: %v", err)
	}
	app.Shutdown()
}

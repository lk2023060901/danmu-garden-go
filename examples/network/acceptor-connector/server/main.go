package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/gorilla/websocket"

	"github.com/lk2023060901/danmu-garden-go/application"
	pingpong "github.com/lk2023060901/danmu-garden-go/examples/network/acceptor-connector/proto"
	"github.com/lk2023060901/danmu-garden-go/internal/network/acceptor"
	"github.com/lk2023060901/danmu-garden-go/internal/network/codec"
	"github.com/lk2023060901/danmu-garden-go/internal/network/compressor"
	networkerr "github.com/lk2023060901/danmu-garden-go/internal/network"
	"github.com/lk2023060901/danmu-garden-go/internal/network/framer"
	"github.com/lk2023060901/danmu-garden-go/internal/network/serializer"
	"github.com/lk2023060901/danmu-garden-go/internal/network/session"
	"github.com/lk2023060901/danmu-garden-go/pkg/util/conc"
)

const (
	opPing uint32 = 1
	opPong uint32 = 2
)

// pingPongHandler 实现 AcceptorHandler，演示 proto ping/pong。
type pingPongHandler struct{}

func (h *pingPongHandler) OnConnected(sess session.ServerSession[uint64]) {
	fmt.Printf("[server] connected: id=%v remote=%v local=%v\n", sess.ID(), sess.RemoteAddr(), sess.LocalAddr())
}

func (h *pingPongHandler) OnMessage(sess session.ServerSession[uint64], header *session.MessageHeader, payload []byte) {
	switch header.Op {
	case opPing:
		var req pingpong.Ping
		var ser serializer.ProtoSerializer
		if err := ser.Unmarshal(payload, &req); err != nil {
			fmt.Printf("[server] failed to unmarshal Ping: %v\n", err)
			return
		}
		fmt.Printf("[server] recv ping from client: %q\n", req.GetMsg())

		resp := &pingpong.Pong{Msg: "pong"}
		fmt.Printf("[server] send pong to client: %q\n", resp.GetMsg())
		if err := sess.Send(opPong, resp); err != nil {
			fmt.Printf("[server] send pong failed: %v\n", err)
		}
	case opPong:
		// 这里通常不会收到 pong，打印一下用于调试。
		fmt.Printf("[server] unexpected pong with seq=%d\n", header.Seq)
	default:
		fmt.Printf("[server] unknown op=%d\n", header.Op)
	}
}

func (h *pingPongHandler) OnClosed(sess session.ServerSession[uint64], err error) {
	fmt.Printf("[server] closed: id=%v err=%v\n", sess.ID(), err)
}

func (h *pingPongHandler) OnError(sess session.ServerSession[uint64], stage networkerr.Stage, err error) {
	id := any(nil)
	if sess != nil {
		id = sess.ID()
	}
	fmt.Printf("[server] error: id=%v stage=%s err=%v\n", id, stage, err)
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

func main() {
	app := application.New()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 使用 Application 的信号抽象来触发优雅退出。
	app.OnSignal(func(kind application.SignalKind, sig os.Signal) {
		if kind == application.SignalShutdown {
			fmt.Println("[server] shutdown signal received")
			cancel()
		}
	})

	addr := "127.0.0.1:29090"
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("[server] listen failed: %v", err)
	}
	fmt.Printf("[server] listening on %s\n", addr)

	acc := acceptor.NewWSAcceptor[uint64](acceptor.Config{
		Path:  "/ws",
		Codec: buildCodec(),
		Upgrader: &websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}, &session.Uint64IDGenerator{})

	handler := &pingPongHandler{}

	// 启动监听器。
	conc.Go(func() (struct{}, error) {
		if err := acc.Serve(ctx, ln, handler); err != nil && err != context.Canceled {
			log.Printf("[server] acceptor serve failed: %v\n", err)
		}
		return struct{}{}, nil
	})

	// 交由 Application 统一等待和处理退出信号。
	if err := app.Run(); err != nil && err != context.Canceled {
		log.Fatalf("[server] application run failed: %v\n", err)
	}
	app.Shutdown()
}

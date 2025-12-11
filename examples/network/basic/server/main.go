package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gorilla/websocket"

	"github.com/lk2023060901/danmu-garden-go/internal/network/acceptor"
	"github.com/lk2023060901/danmu-garden-go/internal/network/codec"
	"github.com/lk2023060901/danmu-garden-go/internal/network/compressor"
	"github.com/lk2023060901/danmu-garden-go/internal/network/framer"
	"github.com/lk2023060901/danmu-garden-go/internal/network/router"
	"github.com/lk2023060901/danmu-garden-go/internal/network/serializer"
	"github.com/lk2023060901/danmu-garden-go/internal/network/session"
	networkerr "github.com/lk2023060901/danmu-garden-go/internal/network"
)

// simpleHandler 是示例用的 AcceptorHandler 实现。
type simpleHandler struct {
	r router.Router
}

func (h *simpleHandler) OnConnected(sess session.ServerSession[uint64]) {
	fmt.Printf("[server] connected: id=%v remote=%v local=%v\n", sess.ID(), sess.RemoteAddr(), sess.LocalAddr())
}

func (h *simpleHandler) OnMessage(sess session.ServerSession[uint64], header *session.MessageHeader, payload []byte) {
	// 在实际项目中你会调用 router.Handle，这里只简单打印。
	fmt.Printf("[server] recv: id=%v op=%d seq=%d payload_len=%d\n", sess.ID(), header.Op, header.Seq, len(payload))
}

func (h *simpleHandler) OnClosed(sess session.ServerSession[uint64], err error) {
	fmt.Printf("[server] closed: id=%v err=%v\n", sess.ID(), err)
}

func (h *simpleHandler) OnError(sess session.ServerSession[uint64], stage networkerr.Stage, err error) {
	fmt.Printf("[server] error: id=%v stage=%s err=%v\n", sess.ID(), stage, err)
}

func buildCodec() codec.Codec {
	c, err := codec.New(codec.Options{
		Framer:     framer.NewLengthPrefixedFramer(0),
		Serializer: serializer.ProtoSerializer{},
		Compressor: compressor.NopCompressor{},
		// Encryptor 可以按需接入，这里用默认 Nop。
	})
	if err != nil {
		log.Fatalf("build codec failed: %v", err)
	}
	return c
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 监听本地端口。
	ln, err := net.Listen("tcp", "127.0.0.1:19090")
	if err != nil {
		log.Fatalf("listen failed: %v", err)
	}
	fmt.Printf("[server] listening on %s\n", ln.Addr())

	// 构造 Router（这里只是展示，实际没有注册任何路由）。
	r := router.New(serializer.ProtoSerializer{})
	_ = r

	// 构造 Acceptor。
	acc := acceptor.NewWSAcceptor[uint64](acceptor.Config{
		Path: "/ws",
		Codec: buildCodec(),
		Upgrader: &websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}, &session.Uint64IDGenerator{})

	h := &simpleHandler{r: r}

	// 优雅退出处理。
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("[server] shutting down...")
		cancel()
	}()

	if err := acc.Serve(ctx, ln, h); err != nil && err != context.Canceled {
		log.Fatalf("acceptor serve failed: %v", err)
	}
}

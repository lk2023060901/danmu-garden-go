package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lk2023060901/danmu-garden-go/internal/network/codec"
	"github.com/lk2023060901/danmu-garden-go/internal/network/compressor"
	"github.com/lk2023060901/danmu-garden-go/internal/network/connector"
	"github.com/lk2023060901/danmu-garden-go/internal/network/framer"
	"github.com/lk2023060901/danmu-garden-go/internal/network/serializer"
	networkerr "github.com/lk2023060901/danmu-garden-go/internal/network"
	"github.com/lk2023060901/danmu-garden-go/internal/network/session"
	protos "github.com/lk2023060901/danmu-garden-game-protos"
)

type simpleClientHandler struct{}

func (h *simpleClientHandler) OnConnected(conn connector.ClientConn) {
	fmt.Printf("[client] connected: remote=%v local=%v\n", conn.RemoteAddr(), conn.LocalAddr())
}

func (h *simpleClientHandler) OnMessage(conn connector.ClientConn, header *session.MessageHeader, payload []byte) {
	fmt.Printf("[client] recv: op=%d seq=%d len=%d\n", header.Op, header.Seq, len(payload))
}

func (h *simpleClientHandler) OnClosed(conn connector.ClientConn, err error) {
	fmt.Printf("[client] closed: err=%v\n", err)
}

func (h *simpleClientHandler) OnError(conn connector.ClientConn, stage networkerr.Stage, err error) {
	fmt.Printf("[client] error: stage=%s err=%v\n", stage, err)
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 构造 Connector。
	connectorCfg := connector.Config{
		Codec:        buildCodec(),
		SendQueueSize: 1024,
		RecvQueueSize: 1024,
	}
	connector := connector.NewWSConnector(connectorCfg)
	h := &simpleClientHandler{}

	// 连接到 server。
	url := "ws://127.0.0.1:19090/ws"
	c, err := connector.Dial(ctx, url, h, http.Header{})
	if err != nil {
		log.Fatalf("dial failed: %v", err)
	}

	// 示例：发送一条简单消息（这里只是发一个空 Envelope）。
	header := &session.MessageHeader{
		Op:        1,
		Seq:       1,
		Timestamp: time.Now().UnixNano(),
	}
	env := &protos.Envelope{
		Header:  header,
		Payload: []byte("hello from client"),
	}
	if err := c.Send(1, env); err != nil {
		log.Printf("send failed: %v", err)
	}

	// 等待信号退出。
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	_ = c.Close()
	fmt.Println("[client] exit")
}


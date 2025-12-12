package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	networkerr "github.com/lk2023060901/danmu-garden-go/internal/network"
	"github.com/lk2023060901/danmu-garden-go/internal/network/acceptor"
	"github.com/lk2023060901/danmu-garden-go/internal/network/codec"
	"github.com/lk2023060901/danmu-garden-go/internal/network/compressor"
	"github.com/lk2023060901/danmu-garden-go/internal/network/framer"
	"github.com/lk2023060901/danmu-garden-go/internal/network/serializer"
	netsession "github.com/lk2023060901/danmu-garden-go/internal/network/session"
	sessutil "github.com/lk2023060901/danmu-garden-go/internal/util/sessionutil"
	etcdutil "github.com/lk2023060901/danmu-garden-go/pkg/util/etcd"
)

const (
	gatewayServerName = "gateway"
)

type gatewayHandler struct{}

func (h *gatewayHandler) OnConnected(sess netsession.ServerSession[uint64]) {
	fmt.Printf("[gateway] connected: id=%v remote=%v local=%v\n",
		sess.ID(), sess.RemoteAddr(), sess.LocalAddr())
}

func (h *gatewayHandler) OnMessage(sess netsession.ServerSession[uint64], header *netsession.MessageHeader, payload []byte) {
	fmt.Printf("[gateway] recv: id=%v op=%d len=%d\n", sess.ID(), header.Op, len(payload))
}

func (h *gatewayHandler) OnClosed(sess netsession.ServerSession[uint64], err error) {
	fmt.Printf("[gateway] closed: id=%v err=%v\n", sess.ID(), err)
}

func (h *gatewayHandler) OnError(sess netsession.ServerSession[uint64], stage networkerr.Stage, err error) {
	if sess != nil {
		fmt.Printf("[gateway] error: id=%v stage=%s err=%v\n", sess.ID(), stage, err)
	} else {
		fmt.Printf("[gateway] error: id=<nil> stage=%s err=%v\n", stage, err)
	}
}

func buildCodec() codec.Codec {
	c, err := codec.New(codec.Options{
		Framer:     framer.NewLengthPrefixedFramer(0),
		Serializer: serializer.ProtoSerializer{},
		Compressor: compressor.NopCompressor{},
	})
	if err != nil {
		log.Fatalf("[gateway] build codec failed: %v", err)
	}
	return c
}

func parseEtcdEndpoints() []string {
	raw := os.Getenv("ETCD_ENDPOINTS")
	if strings.TrimSpace(raw) == "" {
		return []string{"127.0.0.1:2379"}
	}
	items := strings.Split(raw, ",")
	var out []string
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	if len(out) == 0 {
		out = []string{"127.0.0.1:2379"}
	}
	return out
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 信号处理：Ctrl+C 时优雅退出。
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		fmt.Printf("[gateway] signal received: %v, shutting down...\n", sig)
		cancel()
	}()

	// 创建 etcd 客户端。
	endpoints := parseEtcdEndpoints()
	cli, err := etcdutil.GetRemoteEtcdClient(endpoints)
	if err != nil {
		log.Fatalf("[gateway] create etcd client failed: %v", err)
	}
	defer cli.Close()

	// 监听本地随机端口（port=0 交由系统分配）。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("[gateway] listen failed: %v", err)
	}
	defer ln.Close()

	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		log.Fatalf("[gateway] unexpected listener addr type: %T", ln.Addr())
	}
	address := fmt.Sprintf("%s:%d", tcpAddr.IP.String(), tcpAddr.Port)
	fmt.Printf("[gateway] listening on %s\n", address)

	// 基于 sessionutil 在 etcd 中注册当前服务。
	sess := sessutil.NewSessionWithEtcd(ctx, "", cli)
	// 多实例场景下，exclusive=false，允许 gateway-1、gateway-2 等共存。
	sess.Init(gatewayServerName, address, false, false)
	if sess.ServerLabels == nil {
		sess.ServerLabels = make(map[string]string)
	}
	sess.ServerLabels["description"] = "gateway service example using etcd sessionutil"
	sess.ServerLabels["created_at"] = time.Now().Format(time.RFC3339)

	// 注册并启动 keepalive。
	sess.Register()
	defer func() {
		_ = sess.GoingStop()
		sess.Stop()
		fmt.Println("[gateway] session stopped")
	}()

	// 启动 Acceptor，处理 WebSocket 连接。
	acc := acceptor.NewWSAcceptor[uint64](acceptor.Config{
		Path:  "/ws",
		Codec: buildCodec(),
	}, &netsession.Uint64IDGenerator{})
	defer acc.Close()

	handler := &gatewayHandler{}
	go func() {
		if err := acc.Serve(ctx, ln, handler); err != nil && err != context.Canceled {
			log.Printf("[gateway] acceptor serve failed: %v\n", err)
			cancel()
		}
	}()

	// 阻塞直到收到退出信号。
	<-ctx.Done()
	fmt.Println("[gateway] exit")
}


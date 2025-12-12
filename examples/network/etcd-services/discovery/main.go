package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	networkerr "github.com/lk2023060901/danmu-garden-go/internal/network"
	"github.com/lk2023060901/danmu-garden-go/internal/network/codec"
	"github.com/lk2023060901/danmu-garden-go/internal/network/compressor"
	"github.com/lk2023060901/danmu-garden-go/internal/network/connector"
	"github.com/lk2023060901/danmu-garden-go/internal/network/framer"
	"github.com/lk2023060901/danmu-garden-go/internal/network/serializer"
	netsession "github.com/lk2023060901/danmu-garden-go/internal/network/session"
	sessutil "github.com/lk2023060901/danmu-garden-go/internal/util/sessionutil"
	etcdutil "github.com/lk2023060901/danmu-garden-go/pkg/util/etcd"
)

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

func buildCodec() codec.Codec {
	c, err := codec.New(codec.Options{
		Framer:     framer.NewLengthPrefixedFramer(0),
		Serializer: serializer.ProtoSerializer{},
		Compressor: compressor.NopCompressor{},
	})
	if err != nil {
		log.Fatalf("[discovery] build codec failed: %v", err)
	}
	return c
}

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

type clientHandler struct {
	role     string
	serverID int64
}

func (h *clientHandler) OnConnected(conn connector.ClientConn) {
	fmt.Printf("[discovery] connected: role=%s serverID=%d remote=%v local=%v\n",
		h.role, h.serverID, conn.RemoteAddr(), conn.LocalAddr())
}

func (h *clientHandler) OnMessage(conn connector.ClientConn, header *netsession.MessageHeader, payload []byte) {
	fmt.Printf("[discovery] recv: role=%s serverID=%d op=%d seq=%d len=%d\n",
		h.role, h.serverID, header.Op, header.Seq, len(payload))
}

func (h *clientHandler) OnClosed(conn connector.ClientConn, err error) {
	fmt.Printf("[discovery] closed: role=%s serverID=%d remote=%v err=%v\n",
		h.role, h.serverID, conn.RemoteAddr(), err)
}

func (h *clientHandler) OnError(conn connector.ClientConn, stage networkerr.Stage, err error) {
	if conn != nil {
		fmt.Printf("[discovery] error: role=%s serverID=%d remote=%v stage=%s err=%v\n",
			h.role, h.serverID, conn.RemoteAddr(), stage, err)
	} else {
		fmt.Printf("[discovery] error: role=%s serverID=%d remote=<nil> stage=%s err=%v\n",
			h.role, h.serverID, stage, err)
	}
}

type serviceKey struct {
	role     string
	serverID int64
}

type serviceConn struct {
	cancel context.CancelFunc
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 信号处理：Ctrl+C 时优雅退出。
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		fmt.Printf("[discovery] signal received: %v, shutting down...\n", sig)
		cancel()
	}()

	endpoints := parseEtcdEndpoints()

	// 展示一次 etcd 集群健康检查。
	status := etcdutil.HealthCheck(false, false, "", "", false, endpoints, "", "", "", "")
	fmt.Printf("[discovery] etcd health: health=%v reason=%s endpoints=%v\n",
		status.Health, status.Reason, endpoints)

	// 创建 etcd 客户端。
	cli, err := etcdutil.GetRemoteEtcdClient(endpoints)
	if err != nil {
		log.Fatalf("[discovery] create etcd client failed: %v", err)
	}
	defer cli.Close()

	// 创建一个 Session 实例，用于做服务发现/Watch，不参与注册。
	sess := sessutil.NewSessionWithEtcd(ctx, "", cli)
	defer sess.Stop()

	connectorCfg := connector.Config{
		Codec:         buildCodec(),
		SendQueueSize: 1024,
		RecvQueueSize: 1024,
	}
	wsConnector := connector.NewWSConnector(connectorCfg)
	rcfg := reconnectConfig{
		baseDelay: 500 * time.Millisecond,
		maxDelay:  5 * time.Second,
	}

	connections := make(map[serviceKey]*serviceConn)
	var connMu sync.Mutex

	startManageConnection := func(role string, s *sessutil.Session) {
		key := serviceKey{role: role, serverID: s.ServerID}

		connMu.Lock()
		if existing, ok := connections[key]; ok {
			// 已存在管理协程，先停止。
			existing.cancel()
		}
		svcCtx, svcCancel := context.WithCancel(ctx)
		connections[key] = &serviceConn{cancel: svcCancel}
		connMu.Unlock()

		go func(role string, sessionCopy *sessutil.Session) {
			url := fmt.Sprintf("ws://%s/ws", sessionCopy.Address)
			attempt := 0
			for {
				select {
				case <-svcCtx.Done():
					fmt.Printf("[discovery] stop managing connection: role=%s serverID=%d\n", role, sessionCopy.ServerID)
					return
				default:
				}

				fmt.Printf("[discovery] dialing %s instance: serverID=%d url=%s (attempt=%d)\n",
					role, sessionCopy.ServerID, url, attempt)

				h := &clientHandler{
					role:     role,
					serverID: sessionCopy.ServerID,
				}
				c, err := wsConnector.Dial(svcCtx, url, h, http.Header{})
				if err != nil {
					fmt.Printf("[discovery] dial %s serverID=%d failed: %v\n", role, sessionCopy.ServerID, err)
					delay := rcfg.nextDelay(attempt)
					time.Sleep(delay)
					attempt++
					continue
				}

				// 连接成功，等待断开或上层取消。
				attempt = 0
				fmt.Printf("[discovery] connection established: role=%s serverID=%d\n", role, sessionCopy.ServerID)
				<-c.Context().Done()
				fmt.Printf("[discovery] connection context done: role=%s serverID=%d\n", role, sessionCopy.ServerID)

				// 若是因为 svcCtx 被取消，则直接退出；否则按重连策略继续。
				select {
				case <-svcCtx.Done():
					return
				default:
				}

				delay := rcfg.nextDelay(attempt)
				fmt.Printf("[discovery] %s serverID=%d disconnected, will reconnect in %v\n",
					role, sessionCopy.ServerID, delay)
				time.Sleep(delay)
				attempt++
			}
		}(role, s)
	}

	stopManageConnection := func(role string, serverID int64) {
		key := serviceKey{role: role, serverID: serverID}
		connMu.Lock()
		if existing, ok := connections[key]; ok {
			existing.cancel()
			delete(connections, key)
			fmt.Printf("[discovery] stop connection manager by etcd event: role=%s serverID=%d\n", role, serverID)
		}
		connMu.Unlock()
	}

	defer func() {
		connMu.Lock()
		for key, sc := range connections {
			fmt.Printf("[discovery] cleanup connection manager: role=%s serverID=%d\n", key.role, key.serverID)
			sc.cancel()
			delete(connections, key)
		}
		connMu.Unlock()
	}()

	targets := []string{"login", "gateway"}
	for _, name := range targets {
		services, rev, err := sess.GetSessions(ctx, name)
		if err != nil {
			log.Printf("[discovery] GetSessions(%s) failed: %v\n", name, err)
			continue
		}
		fmt.Printf("[discovery] initial services for %q (revision=%d):\n", name, rev)
		for key, s := range services {
			fmt.Printf("  key=%s serverID=%d address=%s labels=%v\n",
				key, s.ServerID, s.Address, s.ServerLabels)
		}

		// 对当前已有实例启动连接管理。
		for _, s := range services {
			startManageConnection(name, s)
		}

		// Watch 该服务角色的上下线变化，打印事件并同步连接。
		watcher := sess.WatchServices(name, rev, nil)
		go func(role string) {
			for {
				select {
				case <-ctx.Done():
					watcher.Stop()
					return
				case ev, ok := <-watcher.EventChannel():
					if !ok {
						return
					}
					fmt.Printf("[discovery] watch event: role=%s type=%s serverID=%d address=%s labels=%v\n",
						role, ev.EventType.String(), ev.Session.ServerID, ev.Session.Address, ev.Session.ServerLabels)
					switch ev.EventType {
					case sessutil.SessionAddEvent:
						startManageConnection(role, ev.Session)
					case sessutil.SessionDelEvent:
						stopManageConnection(role, ev.Session.ServerID)
					}
				}
			}
		}(name)
	}

	// 阻塞直到退出。
	<-ctx.Done()
	fmt.Println("[discovery] exit")
}

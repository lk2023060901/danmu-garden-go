// Licensed to the LF AI & Data foundation under one
// or more contributor license agreements. See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership. The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License. You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package sessionutil

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/blang/semver/v4"
	"github.com/cenkalti/backoff/v4"
	"github.com/cockroachdb/errors"
	"go.etcd.io/etcd/api/v3/mvccpb"
	v3rpc "go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/atomic"
	"go.uber.org/zap"

	"github.com/lk2023060901/danmu-garden-go/internal/json"
	"github.com/lk2023060901/danmu-garden-go/pkg/log"
	"github.com/lk2023060901/danmu-garden-go/pkg/util/merr"
	"github.com/lk2023060901/danmu-garden-go/pkg/util/retry"
)

const (
	// DefaultServiceRoot 为 Session 在 kv 中使用的默认根路径。
	DefaultServiceRoot = "session/"
	// DefaultIDKey 为 Session 使用的默认自增 ID 键名。
	DefaultIDKey                        = "id"
	SupportedLabelPrefix                = "ZEUS_SERVER_LABEL_"
	LabelStreamingNodeEmbeddedQueryNode = "QUERYNODE_STREAMING-EMBEDDED"
	LabelStandalone                     = "STANDALONE"
	ZeusNodeIDForTesting                = "ZEUS_NODE_ID_FOR_TESTING"
	exitCodeSessionLeaseExpired         = 1
)

const (
	defaultSessionTTL        int64 = 30
	defaultSessionRetryTimes int64 = 10
)

var defaultSessionVersion = semver.MustParse("0.0.0")

// EnableEmbededQueryNodeLabel 为嵌入式 Query Node 设置服务标签。
func EnableEmbededQueryNodeLabel() {
	os.Setenv(SupportedLabelPrefix+LabelStreamingNodeEmbeddedQueryNode, "1")
}

// EnableStandaloneLabel 为独立模式设置服务标签。
func EnableStandaloneLabel() {
	os.Setenv(SupportedLabelPrefix+LabelStandalone, "1")
}

// SessionEventType 表示 Session 事件类型。
type SessionEventType int

func (t SessionEventType) String() string {
	switch t {
	case SessionAddEvent:
		return "SessionAddEvent"
	case SessionDelEvent:
		return "SessionDelEvent"
	case SessionUpdateEvent:
		return "SessionUpdateEvent"
	default:
		return ""
	}
}

// Rewatch 定义外部在处理 ErrCompacted 时对 Session 列表的重放行为。
// 调用方应在此函数中完整处理当前 Session 列表，如发生元数据错误则返回 error。
type Rewatch func(sessions map[string]*Session) error

const (
	// SessionNoneEvent 为零值占位事件。
	SessionNoneEvent SessionEventType = iota
	// SessionAddEvent 表示有新的 Session 被添加。
	SessionAddEvent
	// SessionDelEvent 表示有 Session 被删除。
	SessionDelEvent
	// SessionUpdateEvent 表示 Session 状态发生更新（如正在停止）。
	SessionUpdateEvent
)

type IndexEngineVersion struct {
	MinimalIndexVersion int32 `json:"MinimalIndexVersion,omitempty"`
	CurrentIndexVersion int32 `json:"CurrentIndexVersion,omitempty"`
}

// SessionRaw 为 Session 的持久化部分。
type SessionRaw struct {
	ServerID                 int64  `json:"ServerID,omitempty"`
	ServerName               string `json:"ServerName,omitempty"`
	Address                  string `json:"Address,omitempty"`
	Exclusive                bool   `json:"Exclusive,omitempty"`
	Stopping                 bool   `json:"Stopping,omitempty"`
	TriggerKill              bool
	Version                  string             `json:"Version"`
	IndexEngineVersion       IndexEngineVersion `json:"IndexEngineVersion,omitempty"`
	ScalarIndexEngineVersion IndexEngineVersion `json:"ScalarIndexEngineVersion,omitempty"`
	IndexNonEncoding         bool               `json:"IndexNonEncoding,omitempty"`
	LeaseID                  *clientv3.LeaseID  `json:"LeaseID,omitempty"`

	HostName     string            `json:"HostName,omitempty"`
	ServerLabels map[string]string `json:"ServerLabels,omitempty"`
}

func (s *SessionRaw) GetAddress() string {
	return s.Address
}

func (s *SessionRaw) GetServerID() int64 {
	return s.ServerID
}

func (s *SessionRaw) GetServerLabel() map[string]string {
	return s.ServerLabels
}

func (s *SessionRaw) IsTriggerKill() bool {
	return s.TriggerKill
}

// Session 用于存储服务的会话信息，包括 ServerID、ServerName、Address 等。
// Exclusive 表示该服务是否只能启动单实例。
// TODO: 当前同时承担“服务注册”和“服务发现”两种职责，设计上应拆分为两个独立结构。
type Session struct {
	log.Binder

	ctx    context.Context
	cancel context.CancelFunc

	SessionRaw

	Version semver.Version `json:"Version,omitempty"`

	etcdCli           *clientv3.Client
	watchSessionKeyCh clientv3.WatchChan
	watchCancel       atomic.Pointer[context.CancelFunc]
	wg                sync.WaitGroup

	metaRoot string

	registered   atomic.Value
	disconnected atomic.Value

	isStandby           atomic.Value
	enableActiveStandBy bool
	activeKey           string

	sessionTTL        int64
	sessionRetryTimes int64
	reuseNodeID       bool
}

type SessionOption func(session *Session)

func WithTTL(ttl int64) SessionOption {
	return func(session *Session) { session.sessionTTL = ttl }
}

func WithRetryTimes(n int64) SessionOption {
	return func(session *Session) { session.sessionRetryTimes = n }
}

func WithResueNodeID(b bool) SessionOption {
	return func(session *Session) { session.reuseNodeID = b }
}

// WithIndexEngineVersion 仅应由 QueryNode 使用，用于填充索引引擎版本信息。
func WithIndexEngineVersion(minimal, current int32) SessionOption {
	return func(session *Session) {
		session.IndexEngineVersion.MinimalIndexVersion = minimal
		session.IndexEngineVersion.CurrentIndexVersion = current
	}
}

// WithScalarIndexEngineVersion 仅应由 QueryNode 使用，用于填充标量索引引擎版本信息。
func WithScalarIndexEngineVersion(minimal, current int32) SessionOption {
	return func(session *Session) {
		session.ScalarIndexEngineVersion.MinimalIndexVersion = minimal
		session.ScalarIndexEngineVersion.CurrentIndexVersion = current
	}
}

func WithIndexNonEncoding() SessionOption {
	return func(session *Session) {
		session.IndexNonEncoding = true
	}
}

func (s *Session) apply(opts ...SessionOption) {
	for _, opt := range opts {
		opt(s)
	}
}

// UnmarshalJSON 将 JSON 字节反序列化为 Session。
func (s *Session) UnmarshalJSON(data []byte) error {
	err := json.Unmarshal(data, &s.SessionRaw)
	if err != nil {
		return err
	}

	if s.SessionRaw.Version != "" {
		s.Version, err = semver.Parse(s.SessionRaw.Version)
		if err != nil {
			return err
		}
	}

	return nil
}

// MarshalJSON 将 Session 序列化为 JSON 字节。
func (s *Session) MarshalJSON() ([]byte, error) {
	s.SessionRaw.Version = s.Version.String()
	return json.Marshal(s.SessionRaw)
}

// NewSession 创建一个新的 Session 对象。
func NewSession(ctx context.Context, opts ...SessionOption) *Session {
	// client, path := kvfactory.GetEtcdAndPath()
	return NewSessionWithEtcd(ctx, "", nil, opts...)
}

// NewSessionWithEtcd 是创建 Session 的辅助函数。
// ServerID、ServerName、Address、Exclusive 等字段会在 Init 调用后赋值。
// metaRoot 为在 etcd 中保存 Session 信息的路径前缀。
// client 为外部传入的 etcd 客户端。
func NewSessionWithEtcd(ctx context.Context, metaRoot string, client *clientv3.Client, opts ...SessionOption) *Session {
	hostName, hostNameErr := os.Hostname()
	if hostNameErr != nil {
		log.Ctx(ctx).Error("get host name fail", zap.Error(hostNameErr))
	}

	ctx, cancel := context.WithCancel(ctx)
	session := &Session{
		ctx:    ctx,
		cancel: cancel,

		metaRoot: metaRoot,
		Version:  defaultSessionVersion,

		SessionRaw: SessionRaw{
			HostName: hostName,
		},

		// options：Session 的默认配置。
		sessionTTL:        defaultSessionTTL,
		sessionRetryTimes: defaultSessionRetryTimes,
		reuseNodeID:       true,
	}

	session.apply(opts...)

	session.UpdateRegistered(false)
	session.etcdCli = client
	return session
}

// Init 初始化 Session 的基础字段：ServerName、ServerID、Address、Exclusive 等。
// 其中 ServerID 通过 getServerID 获取。
func (s *Session) Init(serverName, address string, exclusive bool, triggerKill bool) {
	s.ServerName = serverName
	s.Address = address
	s.Exclusive = exclusive
	s.TriggerKill = triggerKill
	s.checkIDExist()
	serverID, err := s.getServerID()
	if err != nil {
		panic(err)
	}
	s.ServerID = serverID
	s.ServerLabels = GetServerLabelsFromEnv(serverName)

	s.SetLogger(log.With(
		log.FieldComponent("service-registration"),
		zap.String("role", serverName),
		zap.Int64("serverID", s.ServerID),
		zap.String("address", address),
	))
}

// String 返回 Session 的字符串表示形式，便于日志打印。
func (s *Session) String() string {
	return fmt.Sprintf("Session:<ServerID: %d, ServerName: %s, Version: %s>", s.ServerID, s.ServerName, s.Version.String())
}

// Register 在 etcd 中注册服务并启动 keepalive 循环。
func (s *Session) Register() {
	err := s.registerService()
	if err != nil {
		s.Logger().Error("register failed", zap.Error(err))
		panic(err)
	}
	s.UpdateRegistered(true)
	s.startKeepAliveLoop()
}

var serverIDMu sync.Mutex

func (s *Session) getServerID() (int64, error) {
	serverIDMu.Lock()
	defer serverIDMu.Unlock()

	log.Ctx(s.ctx).Debug("getServerID", zap.Bool("reuse", s.reuseNodeID))
	if s.reuseNodeID {
		// 注意：在独立模式下，所有进程共享同一个 nodeID。
		// 下面的逻辑当前被注释掉，仅保留作为设计参考。
		// if nodeID := paramtable.GetNodeID(); nodeID != 0 {
		// 	return nodeID, nil
		// }
	}
	nodeID, err := s.getServerIDWithKey(DefaultIDKey)
	if err != nil {
		return nodeID, err
	}
	if s.reuseNodeID {
		// paramtable.SetNodeID(nodeID)
	}
	return nodeID, nil
}

func GetServerLabelsFromEnv(role string) map[string]string {
	ret := make(map[string]string)
	switch role {
	case "querynode":
		for _, value := range os.Environ() {
			rs := []rune(value)
			in := strings.Index(value, "=")
			key := string(rs[0:in])
			value := string(rs[in+1:])

			if strings.HasPrefix(key, SupportedLabelPrefix) {
				label := strings.TrimPrefix(key, SupportedLabelPrefix)
				ret[label] = value
			}
		}
	}
	return ret
}

func (s *Session) checkIDExist() {
	s.etcdCli.Txn(s.ctx).If(
		clientv3.Compare(
			clientv3.Version(path.Join(s.metaRoot, DefaultServiceRoot, DefaultIDKey)),
			"=",
			0)).
		Then(clientv3.OpPut(path.Join(s.metaRoot, DefaultServiceRoot, DefaultIDKey), "1")).Commit()
}

func (s *Session) getServerIDWithKey(key string) (int64, error) {
	if os.Getenv(ZeusNodeIDForTesting) != "" {
		log.Info("use node id for testing", zap.String("nodeID", os.Getenv(ZeusNodeIDForTesting)))
		return strconv.ParseInt(os.Getenv(ZeusNodeIDForTesting), 10, 64)
	}
	log := log.Ctx(s.ctx)
	for {
		getResp, err := s.etcdCli.Get(s.ctx, path.Join(s.metaRoot, DefaultServiceRoot, key))
		if err != nil {
			log.Warn("Session get etcd key error", zap.String("key", key), zap.Error(err))
			return -1, err
		}
		if getResp.Count <= 0 {
			log.Warn("Session there is no value", zap.String("key", key))
			continue
		}
		value := string(getResp.Kvs[0].Value)
		valueInt, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			log.Warn("Session ParseInt error", zap.String("value", value), zap.Error(err))
			continue
		}
		txnResp, err := s.etcdCli.Txn(s.ctx).If(
			clientv3.Compare(
				clientv3.Value(path.Join(s.metaRoot, DefaultServiceRoot, key)),
				"=",
				value)).
			Then(clientv3.OpPut(path.Join(s.metaRoot, DefaultServiceRoot, key), strconv.FormatInt(valueInt+1, 10))).Commit()
		if err != nil {
			log.Warn("Session Txn failed", zap.String("key", key), zap.Error(err))
			return -1, err
		}

		if !txnResp.Succeeded {
			log.Warn("Session Txn unsuccessful", zap.String("key", key))
			continue
		}
		log.Debug("Session get serverID success", zap.String("key", key), zap.Int64("ServerId", valueInt))
		return valueInt, nil
	}
}

func (s *Session) getCompleteKey() string {
	key := s.ServerName
	if !s.Exclusive || (s.enableActiveStandBy && s.isStandby.Load().(bool)) {
		key = fmt.Sprintf("%s-%d", key, s.ServerID)
	}
	return path.Join(s.metaRoot, DefaultServiceRoot, key)
}

// registerService 将服务注册到 etcd，使其他服务能发现该服务在线并进行后续操作。
// 以键值形式写入 etcd：
//   key: metaRootPath + "/services" + "/ServerName-ServerID"
//   value: JSON 序列化后的 Session 信息。
//
// Exclusive 表示是否允许同一服务名并存多个实例。
func (s *Session) registerService() error {
	if s.enableActiveStandBy {
		s.updateStandby(true)
	}
	completeKey := s.getCompleteKey()
	s.Logger().Info("service begin to register to etcd")

	registerFn := func() error {
		resp, err := s.etcdCli.Grant(s.ctx, s.sessionTTL)
		if err != nil {
			s.Logger().Error("register service: failed to grant lease from etcd", zap.Error(err))
			return err
		}
		s.LeaseID = &resp.ID

		sessionJSON, err := json.Marshal(s)
		if err != nil {
			s.Logger().Error("register service: failed to marshal session", zap.Error(err))
			return err
		}

		txnResp, err := s.etcdCli.Txn(s.ctx).If(
			clientv3.Compare(
				clientv3.Version(completeKey),
				"=",
				0)).
			Then(clientv3.OpPut(completeKey, string(sessionJSON), clientv3.WithLease(resp.ID))).Commit()
		if err != nil {
			s.Logger().Warn("register on etcd error, check the availability of etcd", zap.Error(err))
			return err
		}
		if txnResp != nil && !txnResp.Succeeded {
			s.handleRestart(completeKey)
			return fmt.Errorf("function CompareAndSwap error for compare is false for key: %s", s.ServerName)
		}
		s.Logger().Info("put session key into etcd, service registered successfully", zap.String("key", completeKey), zap.String("value", string(sessionJSON)))
		return nil
	}
	return retry.Do(s.ctx, registerFn, retry.Attempts(uint(s.sessionRetryTimes)))
}

// handleRestart 是处理节点重启的快速路径。
// 仅供协调组件使用：如果发现旧 Session 与当前节点地址相同，则删除旧 Session 以加快恢复。
func (s *Session) handleRestart(key string) {
	resp, err := s.etcdCli.Get(s.ctx, key)
	log := log.With(zap.String("key", key))
	if err != nil {
		log.Warn("failed to read old session from etcd, ignore", zap.Error(err))
		return
	}
	for _, kv := range resp.Kvs {
		session := &Session{}
		err = json.Unmarshal(kv.Value, session)
		if err != nil {
			log.Warn("failed to unmarshal old session from etcd, ignore", zap.Error(err))
			return
		}

		if session.Address == s.Address && session.ServerID < s.ServerID {
			log.Warn("find old session is same as current node, assume it as restart, purge old session", zap.String("key", key),
				zap.String("address", session.Address))
			_, err := s.etcdCli.Delete(s.ctx, key)
			if err != nil {
				log.Warn("failed to unmarshal old session from etcd, ignore", zap.Error(err))
				return
			}
		}
	}
}

// processKeepAliveResponse 处理 etcd KeepAlive 的响应。
// 若 KeepAlive 因异常失败，会尝试回收租约并退出循环。
func (s *Session) processKeepAliveResponse() {
	defer func() {
		s.Logger().Info("keep alive loop exited successfully, try to revoke lease right away...")
		// 此时 s.ctx 可能已经结束，因此使用带超时的 context.Background() 来撤销租约。
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if _, err := s.etcdCli.Revoke(ctx, *s.LeaseID); err != nil {
			s.Logger().Error("failed to revoke lease", zap.Error(err), zap.Int64("leaseID", int64(*s.LeaseID)))
		}
		s.Logger().Info("lease revoked successfully", zap.Int64("leaseID", int64(*s.LeaseID)))
		s.wg.Done()
	}()

	backoff := backoff.NewExponentialBackOff()
	backoff.InitialInterval = 10 * time.Millisecond
	backoff.MaxInterval = 100 * time.Second
	backoff.MaxElapsedTime = 0
	backoff.Reset()

	var ch <-chan *clientv3.LeaseKeepAliveResponse
	var lastErr error
	nextKeepaliveInstant := time.Now().Add(time.Duration(s.sessionTTL) * time.Second)

	for {
		if s.ctx.Err() != nil {
			return
		}
		if lastErr != nil {
			nextBackoffInterval := backoff.NextBackOff()
			s.Logger().Warn("failed to start keep alive, wait for retry...", zap.Error(lastErr), zap.Duration("nextBackoffInterval", nextBackoffInterval))
			select {
			case <-time.After(nextBackoffInterval):
			case <-s.ctx.Done():
				return
			}
		}

		if ch == nil {
			if err := s.checkKeepaliveTTL(nextKeepaliveInstant); err != nil {
				lastErr = err
				continue
			}
			newCH, err := s.etcdCli.KeepAlive(s.ctx, *s.LeaseID)
			if err != nil {
				s.Logger().Error("failed to keep alive with etcd", zap.Error(err))
				lastErr = errors.Wrap(err, "failed to keep alive")
				continue
			}
			s.Logger().Info("keep alive...", zap.Int64("leaseID", int64(*s.LeaseID)))
			ch = newCH
		}

		// 阻塞直到 KeepAlive 失败为止。
		for range ch {
		}

		// 接收到 KeepAlive 响应后继续循环。
		// 通道可能因为网络错误被关闭，此时需要重新建立 KeepAlive。
		ch = nil
		nextKeepaliveInstant = time.Now().Add(time.Duration(s.sessionTTL) * time.Second)
		lastErr = nil
		backoff.Reset()
	}
}

// checkKeepaliveTTL 检查当前租约的 TTL。
// 若租约不存在或已过期，则返回错误或直接退出进程。
func (s *Session) checkKeepaliveTTL(nextKeepaliveInstant time.Time) error {
	errSessionExpiredAtClientSide := errors.New("session expired at client side")
	ctx, cancel := context.WithDeadlineCause(s.ctx, nextKeepaliveInstant, errSessionExpiredAtClientSide)
	defer cancel()

	ttlResp, err := s.etcdCli.TimeToLive(ctx, *s.LeaseID)
	if err != nil {
		if errors.Is(err, v3rpc.ErrLeaseNotFound) {
			s.Logger().Error("confirm the lease is not found, the session is expired without activing closing", zap.Error(err))
			os.Exit(exitCodeSessionLeaseExpired)
		}
		if ctx.Err() != nil && errors.Is(context.Cause(ctx), errSessionExpiredAtClientSide) {
			s.Logger().Error("session expired at client side, the session is expired without activing closing", zap.Error(err))
			os.Exit(exitCodeSessionLeaseExpired)
		}
		return errors.Wrap(err, "failed to check TTL")
	}
	if ttlResp.TTL <= 0 {
		s.Logger().Error("confirm the lease is expired, the session is expired without activing closing", zap.Error(err))
		os.Exit(exitCodeSessionLeaseExpired)
	}
	s.Logger().Info("check TTL success, try to keep alive...", zap.Int64("ttl", ttlResp.TTL))
	return nil
}

func (s *Session) startKeepAliveLoop() {
	s.wg.Add(1)
	go s.processKeepAliveResponse()
}

// GetSessions 获取所有已注册到 etcd 的 Session。
// 返回的 Revision 可用于 WatchServices 以避免遗漏事件。
func (s *Session) GetSessions(ctx context.Context, prefix string) (map[string]*Session, int64, error) {
	res := make(map[string]*Session)
	key := path.Join(s.metaRoot, DefaultServiceRoot, prefix)
	resp, err := s.etcdCli.Get(ctx, key, clientv3.WithPrefix(),
		clientv3.WithSort(clientv3.SortByKey, clientv3.SortAscend))
	if err != nil {
		return nil, 0, err
	}
	for _, kv := range resp.Kvs {
		session := &Session{}
		err = json.Unmarshal(kv.Value, session)
		if err != nil {
			return nil, 0, err
		}
		_, mapKey := path.Split(string(kv.Key))
		log.Ctx(s.ctx).Debug("SessionUtil GetSessions",
			zap.String("prefix", prefix),
			zap.String("key", mapKey),
			zap.String("address", session.Address))
		res[mapKey] = session
	}
	return res, resp.Header.Revision, nil
}

// GetSessionsWithVersionRange 获取指定版本范围内、指定前缀下的 Session。
// 返回的 Revision 可用于 WatchServices 以避免遗漏事件。
func (s *Session) GetSessionsWithVersionRange(prefix string, r semver.Range) (map[string]*Session, int64, error) {
	log := log.Ctx(s.ctx)
	res := make(map[string]*Session)
	key := path.Join(s.metaRoot, DefaultServiceRoot, prefix)
	resp, err := s.etcdCli.Get(s.ctx, key, clientv3.WithPrefix(),
		clientv3.WithSort(clientv3.SortByKey, clientv3.SortAscend))
	if err != nil {
		return nil, 0, err
	}
	for _, kv := range resp.Kvs {
		session := &Session{}
		err = json.Unmarshal(kv.Value, session)
		if err != nil {
			return nil, 0, err
		}
		if !r(session.Version) {
			log.Debug("Session version out of range", zap.String("version", session.Version.String()), zap.Int64("serverID", session.ServerID))
			continue
		}
		_, mapKey := path.Split(string(kv.Key))
		log.Debug("SessionUtil GetSessions ", zap.String("prefix", prefix),
			zap.String("key", mapKey),
			zap.String("address", session.Address))
		res[mapKey] = session
	}
	return res, resp.Header.Revision, nil
}

func (s *Session) GoingStop() error {
	if s == nil || s.etcdCli == nil || s.LeaseID == nil {
		return errors.New("the session hasn't been init")
	}

	if s.Disconnected() {
		return errors.New("this session has disconnected")
	}

	completeKey := s.getCompleteKey()
	resp, err := s.etcdCli.Get(s.ctx, completeKey, clientv3.WithCountOnly())
	if err != nil {
		s.Logger().Error("fail to get the session", zap.String("key", completeKey), zap.Error(err))
		return err
	}
	if resp.Count == 0 {
		return nil
	}
	s.Stopping = true
	sessionJSON, err := json.Marshal(s)
	if err != nil {
		s.Logger().Error("fail to marshal the session", zap.String("key", completeKey))
		return err
	}
	_, err = s.etcdCli.Put(s.ctx, completeKey, string(sessionJSON), clientv3.WithLease(*s.LeaseID))
	if err != nil {
		s.Logger().Error("fail to update the session to stopping state", zap.String("key", completeKey))
		return err
	}
	return nil
}

// SessionEvent 表示其他服务的 Session 变更事件。
// 服务上线时 EventType 为 SessionAddEvent，下线时为 SessionDelEvent，状态更新时为 SessionUpdateEvent。
type SessionEvent struct {
	EventType SessionEventType
	Session   *Session
}

type sessionWatcher struct {
	s         *Session
	cancel    context.CancelFunc
	rch       clientv3.WatchChan
	eventCh   chan *SessionEvent
	prefix    string
	rewatch   Rewatch
	validate  func(*Session) bool
	wg        sync.WaitGroup
	closeOnce sync.Once
}

func (w *sessionWatcher) closeEventCh() {
	w.closeOnce.Do(func() {
		close(w.eventCh)
	})
}

func (w *sessionWatcher) start(ctx context.Context) {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case wresp, ok := <-w.rch:
				if !ok {
					w.closeEventCh()
					log.Warn("session watch channel closed")
					return
				}
				w.handleWatchResponse(wresp)
			}
		}
	}()
}

func (w *sessionWatcher) Stop() {
	w.cancel()
	w.wg.Wait()
}

// EmptySessionWatcher 返回一个空实现的 SessionWatcher，占位使用。
func EmptySessionWatcher() SessionWatcher {
	return emptySessionWatcher{}
}

// emptySessionWatcher 为占位用的空实现。
type emptySessionWatcher struct{}

func (emptySessionWatcher) EventChannel() <-chan *SessionEvent {
	return nil
}

func (emptySessionWatcher) Stop() {}

// WatchServices 监听指定前缀下服务在 etcd 中的上下线变化，并将事件发送到 eventChannel。
// prefix 用于标识要监听的服务前缀；revision 用于避免事件遗漏。
func (s *Session) WatchServices(prefix string, revision int64, rewatch Rewatch) (watcher SessionWatcher) {
	ctx, cancel := context.WithCancel(s.ctx)
	w := &sessionWatcher{
		s:        s,
		cancel:   cancel,
		eventCh:  make(chan *SessionEvent, 100),
		rch:      s.etcdCli.Watch(s.ctx, path.Join(s.metaRoot, DefaultServiceRoot, prefix), clientv3.WithPrefix(), clientv3.WithPrevKV(), clientv3.WithRev(revision)),
		prefix:   prefix,
		rewatch:  rewatch,
		validate: func(s *Session) bool { return true },
	}
	w.start(ctx)
	return w
}

// WatchServicesWithVersionRange 与 WatchServices 类似，但会额外校验 Session 的版本范围。
// 仅在版本满足 r 时才会发送事件。
func (s *Session) WatchServicesWithVersionRange(prefix string, r semver.Range, revision int64, rewatch Rewatch) (watcher SessionWatcher) {
	ctx, cancel := context.WithCancel(s.ctx)
	w := &sessionWatcher{
		s:        s,
		cancel:   cancel,
		eventCh:  make(chan *SessionEvent, 100),
		rch:      s.etcdCli.Watch(s.ctx, path.Join(s.metaRoot, DefaultServiceRoot, prefix), clientv3.WithPrefix(), clientv3.WithPrevKV(), clientv3.WithRev(revision)),
		prefix:   prefix,
		rewatch:  rewatch,
		validate: func(s *Session) bool { return r(s.Version) },
	}
	w.start(ctx)
	return w
}

func (w *sessionWatcher) handleWatchResponse(wresp clientv3.WatchResponse) {
	log := log.Ctx(context.TODO())
	if wresp.Err() != nil {
		err := w.handleWatchErr(wresp.Err())
		if err != nil {
			log.Error("failed to handle watch session response", zap.Error(err))
			panic(err)
		}
		return
	}
	for _, ev := range wresp.Events {
		session := &Session{}
		var eventType SessionEventType
		switch ev.Type {
		case mvccpb.PUT:
			log.Debug("watch services",
				zap.Any("add kv", ev.Kv))
			err := json.Unmarshal(ev.Kv.Value, session)
			if err != nil {
				log.Error("watch services", zap.Error(err))
				continue
			}
			if !w.validate(session) {
				continue
			}
			if session.Stopping {
				eventType = SessionUpdateEvent
			} else {
				eventType = SessionAddEvent
			}
		case mvccpb.DELETE:
			log.Debug("watch services",
				zap.Any("delete kv", ev.PrevKv))
			err := json.Unmarshal(ev.PrevKv.Value, session)
			if err != nil {
				log.Error("watch services", zap.Error(err))
				continue
			}
			if !w.validate(session) {
				continue
			}
			eventType = SessionDelEvent
		}
		log.Debug("WatchService", zap.Any("event type", eventType))
		w.eventCh <- &SessionEvent{
			EventType: eventType,
			Session:   session,
		}
	}
}

func (w *sessionWatcher) handleWatchErr(err error) error {
	// if not ErrCompacted, just close the channel
	if err != v3rpc.ErrCompacted {
		// close event channel
		log.Warn("Watch service found error", zap.Error(err))
		w.closeEventCh()
		return err
	}

	sessions, revision, err := w.s.GetSessions(w.s.ctx, w.prefix)
	if err != nil {
		log.Warn("GetSession before rewatch failed", zap.String("prefix", w.prefix), zap.Error(err))
		w.closeEventCh()
		return err
	}
	// rewatch is nil, no logic to handle
	if w.rewatch == nil {
		log.Warn("Watch service with ErrCompacted but no rewatch logic provided")
	} else {
		err = w.rewatch(sessions)
	}
	if err != nil {
		log.Warn("WatchServices rewatch failed", zap.String("prefix", w.prefix), zap.Error(err))
		w.closeEventCh()
		return err
	}

	w.rch = w.s.etcdCli.Watch(w.s.ctx, path.Join(w.s.metaRoot, DefaultServiceRoot, w.prefix), clientv3.WithPrefix(), clientv3.WithPrevKV(), clientv3.WithRev(revision))
	return nil
}

func (w *sessionWatcher) EventChannel() <-chan *SessionEvent {
	return w.eventCh
}

func (s *Session) Stop() {
	log.Info("session stopping", zap.String("serverName", s.ServerName))
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

// UpdateRegistered 更新当前 Session 的注册状态。
func (s *Session) UpdateRegistered(b bool) {
	s.registered.Store(b)
}

// Registered 判断 Session 是否已经注册到 etcd。
func (s *Session) Registered() bool {
	b, ok := s.registered.Load().(bool)
	if !ok {
		return false
	}
	return b
}

func (s *Session) SetDisconnected(b bool) {
	s.disconnected.Store(b)
}

func (s *Session) Disconnected() bool {
	b, ok := s.disconnected.Load().(bool)
	if !ok {
		return false
	}
	return b
}

func (s *Session) SetEnableActiveStandBy(enable bool) {
	s.enableActiveStandBy = enable
}

func (s *Session) updateStandby(b bool) {
	s.isStandby.Store(b)
}

// ProcessActiveStandBy 由协调组件调用，用于实现 Active-Standby 机制。
// 流程：
//   1. 进入 STANDBY 模式；
//   2. 尝试注册 active key；
//   3. 若注册成功，则成为 ACTIVE，退出 STANDBY 模式；
//   4. 若注册失败，说明已有 ACTIVE 节点，则监听 active key，一旦删除则回到步骤 2。
//
// activateFunc 为切换为 ACTIVE 后的回调函数。
func (s *Session) ProcessActiveStandBy(activateFunc func() error) error {
	s.activeKey = path.Join(s.metaRoot, DefaultServiceRoot, s.ServerName)
	log := log.Ctx(s.ctx)
	// try to register to the active_key.
	// return
	//   1. doRegistered: if registered the active_key by this session or by other session
	//   2. revision: revision of the active_key

	registerActiveFn := func() (bool, int64, error) {
		log.Info(fmt.Sprintf("try to register as ACTIVE %v service...", s.ServerName))
		sessionJSON, err := json.Marshal(s)
		if err != nil {
			log.Error("json marshal error", zap.Error(err))
			return false, -1, err
		}
		txnResp, err := s.etcdCli.Txn(s.ctx).If(
			clientv3.Compare(
				clientv3.Version(s.activeKey),
				"=",
				0)).
			Then(clientv3.OpPut(s.activeKey, string(sessionJSON), clientv3.WithLease(*s.LeaseID))).Commit()
		if err != nil {
			log.Error("register active key to etcd failed", zap.Error(err))
			return false, -1, err
		}
		doRegistered := txnResp.Succeeded
		if doRegistered {
			log.Info(fmt.Sprintf("register ACTIVE %s", s.ServerName))
		} else {
			log.Info(fmt.Sprintf("ACTIVE %s has already been registered", s.ServerName))
		}
		revision := txnResp.Header.GetRevision()
		return doRegistered, revision, nil
	}
	s.updateStandby(true)
	log.Info(fmt.Sprintf("serverName: %v enter STANDBY mode", s.ServerName))
	go func() {
		for s.isStandby.Load().(bool) {
			log.Debug(fmt.Sprintf("serverName: %v is in STANDBY ...", s.ServerName))
			time.Sleep(10 * time.Second)
		}
	}()

	for {
		registered, revision, err := registerActiveFn()
		if err != nil {
			if err == merr.ErrOldSessionExists {
				// If old session exists, wait and retry
				time.Sleep(100 * time.Millisecond)
				continue
			}
			// Some error such as ErrLeaseNotFound, is not retryable.
			// Just return error to stop the standby process and wait for retry.
			return err
		}
		if registered {
			break
		}
		log.Info(fmt.Sprintf("%s start to watch ACTIVE key %s", s.ServerName, s.activeKey))
		ctx, cancel := context.WithCancel(s.ctx)
		watchChan := s.etcdCli.Watch(ctx, s.activeKey, clientv3.WithPrevKV(), clientv3.WithRev(revision))
		select {
		case <-ctx.Done():
			cancel()
		case wresp, ok := <-watchChan:
			if !ok {
				cancel()
			}
			if wresp.Err() != nil {
				cancel()
			}
			for _, event := range wresp.Events {
				switch event.Type {
				case mvccpb.PUT:
					log.Debug("watch the ACTIVE key", zap.Any("ADD", event.Kv))
				case mvccpb.DELETE:
					log.Debug("watch the ACTIVE key", zap.Any("DELETE", event.Kv))
					cancel()
				}
			}
		}
		cancel()
		log.Info(fmt.Sprintf("stop watching ACTIVE key %v", s.activeKey))
	}

	s.updateStandby(false)
	log.Info(fmt.Sprintf("serverName: %v quit STANDBY mode, this node will become ACTIVE, ID: %d", s.ServerName, s.ServerID))
	if activateFunc != nil {
		return activateFunc()
	}
	return nil
}

func filterEmptyStrings(s []string) []string {
	var filtered []string
	for _, str := range s {
		if str != "" {
			filtered = append(filtered, str)
		}
	}
	return filtered
}

func GetSessions(pid int) []string {
	fileFullName := GetServerInfoFilePath(pid)
	if _, err := os.Stat(fileFullName); errors.Is(err, os.ErrNotExist) {
		log.Warn("not found server info file path", zap.String("filePath", fileFullName), zap.Error(err))
		return []string{}
	}

	// v, err := storage.ReadFile(fileFullName)
	// if err != nil {
	// 	log.Warn("read server info file path failed", zap.String("filePath", fileFullName), zap.Error(err))
	// 	return []string{}
	// }

	// return filterEmptyStrings(strings.Split(string(v), "\n"))
	return filterEmptyStrings([]string{})
}

func RemoveServerInfoFile(pid int) {
	fullPath := GetServerInfoFilePath(pid)
	_ = os.Remove(fullPath)
}

// GetServerInfoFilePath 返回服务信息文件所在路径，例如：/tmp/zeus/server_id_123456789。
// 注意：该方法暂不支持 Windows。
func GetServerInfoFilePath(pid int) string {
	tmpDir := "/tmp/zeus"
	_ = os.Mkdir(tmpDir, os.ModePerm)
	fileName := fmt.Sprintf("server_id_%d", pid)
	filePath := filepath.Join(tmpDir, fileName)
	return filePath
}

func saveServerInfoInternal(role string, serverID int64, pid int) {
	fileFullPath := GetServerInfoFilePath(pid)
	fd, err := os.OpenFile(fileFullPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o664)
	if err != nil {
		log.Warn("open server info file fail", zap.String("filePath", fileFullPath), zap.Error(err))
		return
	}
	defer fd.Close()

	data := fmt.Sprintf("%s-%d\n", role, serverID)
	_, err = fd.WriteString(data)
	if err != nil {
		log.Warn("write server info file fail", zap.String("filePath", fileFullPath), zap.Error(err))
	}

	log.Info("save server info into file", zap.String("content", data), zap.String("filePath", fileFullPath))
}

func SaveServerInfo(role string, serverID int64) {
	saveServerInfoInternal(role, serverID, os.Getpid())
}

// GetSessionPrefixByRole get session prefix by role
func GetSessionPrefixByRole(role string) string {
	return ""
	// return path.Join(paramtable.Get().EtcdCfg.MetaRootPath.GetValue(), DefaultServiceRoot, role)
}

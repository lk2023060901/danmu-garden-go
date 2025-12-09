// Licensed to the LF AI & Data foundation under one
// or more contributor license agreements. See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership. The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License. You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package sessionutil

import (
	"context"

	"github.com/blang/semver/v4"
)

type SessionInterface interface {
	UnmarshalJSON(data []byte) error
	MarshalJSON() ([]byte, error)

	// Init 初始化 Session 的基础信息，包括服务名、地址、是否独占以及是否触发 Kill。
	Init(serverName, address string, exclusive bool, triggerKill bool)
	// String 返回 Session 的字符串表示，便于日志输出。
	String() string
	// Register 将当前服务注册到 etcd。
	Register()

	// GetSessions 获取指定前缀下所有已注册的 Session。
	GetSessions(ctx context.Context, prefix string) (map[string]*Session, int64, error)
	// GetSessionsWithVersionRange 获取指定版本范围内的 Session 列表。
	GetSessionsWithVersionRange(prefix string, r semver.Range) (map[string]*Session, int64, error)

	// GoingStop 将当前 Session 标记为即将停止。
	GoingStop() error
	// WatchServices 监听指定前缀下服务的上下线变更。
	WatchServices(prefix string, revision int64, rewatch Rewatch) (watcher SessionWatcher)
	// WatchServicesWithVersionRange 在指定版本范围内监听服务的上下线变更。
	WatchServicesWithVersionRange(prefix string, r semver.Range, revision int64, rewatch Rewatch) (watcher SessionWatcher)
	// Stop 停止 Session，释放相关资源。
	Stop()
	// UpdateRegistered 更新当前 Session 的注册状态。
	UpdateRegistered(b bool)
	// Registered 返回当前 Session 是否已注册到 etcd。
	Registered() bool
	// SetDisconnected 标记 Session 是否已断开连接。
	SetDisconnected(b bool)
	// Disconnected 返回 Session 是否已断开连接。
	Disconnected() bool
	// SetEnableActiveStandBy 设置是否启用主动/备用模式。
	SetEnableActiveStandBy(enable bool)
	// ProcessActiveStandBy 执行主动/备用切换逻辑。
	ProcessActiveStandBy(activateFunc func() error) error

	// GetAddress 返回当前 Session 绑定的地址。
	GetAddress() string
	// GetServerID 返回当前 Session 的 ServerID。
	GetServerID() int64
	// IsTriggerKill 返回是否需要触发 Kill 逻辑。
	IsTriggerKill() bool
}

type SessionWatcher interface {
	// EventChannel 返回用于接收 Session 事件的通道。
	EventChannel() <-chan *SessionEvent
	// Stop 停止当前 Watcher，释放资源。
	Stop()
}

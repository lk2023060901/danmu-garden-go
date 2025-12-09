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

package conc

import (
	"time"

	ants "github.com/panjf2000/ants/v2"
	"go.uber.org/zap"

	"github.com/lk2023060901/danmu-garden-go/pkg/log"
)

type poolOption struct {
	// preAlloc 表示是否预先分配 worker。
	preAlloc bool
	// nonBlocking 表示当协程池已满时是否阻塞调用方。
	nonBlocking bool
	// expiryDuration 为清理空闲 worker 协程的时间间隔。
	expiryDuration time.Duration
	// disablePurge 表示是否禁用定期清理 worker。
	disablePurge bool
	// concealPanic 表示当任务发生 panic 时是否吞掉异常。
	concealPanic bool
	// panicHandler 为任务发生 panic 时的自定义处理逻辑。
	panicHandler func(any)

	// preHandler 为实际任务执行前的预处理函数。
	preHandler func()
}

func (opt *poolOption) antsOptions() []ants.Option {
	var result []ants.Option
	result = append(result, ants.WithPreAlloc(opt.preAlloc))
	result = append(result, ants.WithNonblocking(opt.nonBlocking))
	result = append(result, ants.WithDisablePurge(opt.disablePurge))
	// ants 默认会 recover panic，
	// 但不会将错误返回给调用方。
	result = append(result, ants.WithPanicHandler(func(v any) {
		log.Error("Conc pool panicked", zap.Any("panic", v))
		if !opt.concealPanic {
			panic(v)
		}
	}))
	if opt.panicHandler != nil {
		result = append(result, ants.WithPanicHandler(opt.panicHandler))
	}
	if opt.expiryDuration > 0 {
		result = append(result, ants.WithExpiryDuration(opt.expiryDuration))
	}

	return result
}

// PoolOption 用于配置协程池行为的选项函数。
type PoolOption func(opt *poolOption)

func defaultPoolOption() *poolOption {
	return &poolOption{
		preAlloc:       false,
		nonBlocking:    false,
		expiryDuration: 0,
		disablePurge:   false,
		concealPanic:   false,
	}
}

func WithPreAlloc(v bool) PoolOption {
	return func(opt *poolOption) {
		opt.preAlloc = v
	}
}

func WithNonBlocking(v bool) PoolOption {
	return func(opt *poolOption) {
		opt.nonBlocking = v
	}
}

func WithDisablePurge(v bool) PoolOption {
	return func(opt *poolOption) {
		opt.disablePurge = v
	}
}

func WithExpiryDuration(d time.Duration) PoolOption {
	return func(opt *poolOption) {
		opt.expiryDuration = d
	}
}

func WithConcealPanic(v bool) PoolOption {
	return func(opt *poolOption) {
		opt.concealPanic = v
	}
}

func WithPreHandler(fn func()) PoolOption {
	return func(opt *poolOption) {
		opt.preHandler = fn
	}
}

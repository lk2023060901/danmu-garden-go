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

package log

import (
	"sync/atomic"

	"github.com/uber/jaeger-client-go/utils"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// MLogger 是 zap.Logger 的封装类型。
// 在原有 Logger 的基础上，增加了按速率分组的限流日志能力。
type MLogger struct {
	*zap.Logger
	rl atomic.Value // *utils.ReconfigurableRateLimiter
}

// With 封装 zap.Logger 的 With 方法，并返回新的 MLogger 实例。
// 新实例携带额外的字段，不影响原 Logger。
func (l *MLogger) With(fields ...zap.Field) *MLogger {
	nl := &MLogger{
		Logger: l.Logger.WithOptions(zap.WrapCore(func(core zapcore.Core) zapcore.Core {
			return NewLazyWith(core, fields)
		})),
	}
	return nl
}

// WithRateGroup 为当前 Logger 绑定一个命名 RateLimiter。
// 不同 groupName 可以复用或独立配置限流参数。
func (l *MLogger) WithRateGroup(groupName string, creditPerSecond, maxBalance float64) *MLogger {
	rl := utils.NewRateLimiter(creditPerSecond, maxBalance)
	actual, loaded := _namedRateLimiters.LoadOrStore(groupName, rl)
	if loaded {
		rl.Update(creditPerSecond, maxBalance)
		rl = actual.(*utils.ReconfigurableRateLimiter)
	}
	l.rl.Store(rl)
	return l
}

func (l *MLogger) r() RateLimiter {
	val := l.rl.Load()
	if val == nil {
		return R()
	}
	// logger-level limiter stored by WithRateGroup
	if rl, ok := val.(RateLimiter); ok {
		return rl
	}
	// fallback: type from jaeger utils
	if rl, ok := val.(utils.RateLimiter); ok {
		return rl
	}
	return R()
}

// RatedDebug 在 Debug 级别输出限流日志。
// 当限流通过时调用 Debug，并返回 true；否则不输出日志并返回 false。
func (l *MLogger) RatedDebug(cost float64, msg string, fields ...zap.Field) bool {
	if l.r().CheckCredit(cost) {
		l.WithOptions(zap.AddCallerSkip(1)).Debug(msg, fields...)
		return true
	}
	return false
}

// RatedInfo 在 Info 级别输出限流日志。
// 当限流通过时调用 Info，并返回 true；否则不输出日志并返回 false。
func (l *MLogger) RatedInfo(cost float64, msg string, fields ...zap.Field) bool {
	if l.r().CheckCredit(cost) {
		l.WithOptions(zap.AddCallerSkip(1)).Info(msg, fields...)
		return true
	}
	return false
}

// RatedWarn 在 Warn 级别输出限流日志。
// 当限流通过时调用 Warn，并返回 true；否则不输出日志并返回 false。
func (l *MLogger) RatedWarn(cost float64, msg string, fields ...zap.Field) bool {
	if l.r().CheckCredit(cost) {
		l.WithOptions(zap.AddCallerSkip(1)).Warn(msg, fields...)
		return true
	}
	return false
}

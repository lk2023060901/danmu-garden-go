// Copyright (C) 2019-2020 Zilliz. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance
// with the License. You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software distributed under the License
// is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express
// or implied. See the License for the specific language governing permissions and limitations under the License.

package retry

import (
	"context"
	"runtime"
	"strconv"
	"time"

	"github.com/cockroachdb/errors"
	"go.uber.org/zap"

	"github.com/lk2023060901/danmu-garden-go/pkg/log"
	"github.com/lk2023060901/danmu-garden-go/pkg/util/funcutil"
	"github.com/lk2023060901/danmu-garden-go/pkg/util/merr"
)

func getCaller(skip int) string {
	_, file, line, ok := runtime.Caller(skip)
	if !ok {
		return "unknown"
	}
	return file + ":" + strconv.Itoa(line)
}

// Do 使用重试机制执行指定函数。
// fn 为待执行的函数。
// opts 用于控制最大重试次数、初始休眠时间等行为。
func Do(ctx context.Context, fn func() error, opts ...Option) error {
	if !funcutil.CheckCtxValid(ctx) {
		return ctx.Err()
	}

	log := log.Ctx(ctx)
	c := newDefaultConfig()

	for _, opt := range opts {
		opt(c)
	}

	var lastErr error

	for i := uint(0); c.attempts == 0 || i < c.attempts; i++ {
		if err := fn(); err != nil {
			if i%4 == 0 {
				log.Warn("retry func failed",
					zap.Uint("retried", i),
					zap.Error(err),
					zap.String("caller", getCaller(2)))
			}

			if !IsRecoverable(err) {
				isContextErr := errors.IsAny(err, context.Canceled, context.DeadlineExceeded)
				log.Warn("retry func failed, not be recoverable",
					zap.Uint("retried", i),
					zap.Uint("attempt", c.attempts),
					zap.Bool("isContextErr", isContextErr),
					zap.String("caller", getCaller(2)),
				)
				if isContextErr && lastErr != nil {
					return lastErr
				}
				return err
			}
			if c.isRetryErr != nil && !c.isRetryErr(err) {
				log.Warn("retry func failed, not be retryable",
					zap.Uint("retried", i),
					zap.Uint("attempt", c.attempts),
					zap.String("caller", getCaller(2)),
				)
				return err
			}

			deadline, ok := ctx.Deadline()
			if ok && time.Until(deadline) < c.sleep {
				isContextErr := errors.IsAny(err, context.Canceled, context.DeadlineExceeded)
				log.Warn("retry func failed, deadline",
					zap.Uint("retried", i),
					zap.Uint("attempt", c.attempts),
					zap.Bool("isContextErr", isContextErr),
					zap.String("caller", getCaller(2)),
				)
				if isContextErr && lastErr != nil {
					return lastErr
				}
				return err
			}

			lastErr = err

			select {
			case <-time.After(c.sleep):
			case <-ctx.Done():
				log.Warn("retry func failed, ctx done",
					zap.Uint("retried", i),
					zap.Uint("attempt", c.attempts),
					zap.String("caller", getCaller(2)),
				)
				return lastErr
			}

			c.sleep *= 2
			if c.sleep > c.maxSleepTime {
				c.sleep = c.maxSleepTime
			}
		} else {
			return nil
		}
	}
	if lastErr != nil {
		log.Warn("retry func failed, reach max retry",
			zap.Uint("attempt", c.attempts),
		)
	}
	return lastErr
}

// Handle 使用重试机制执行指定函数。
// fn 为待执行的函数，返回 shouldRetry 标记和错误。
// opts 用于控制最大重试次数、初始休眠时间等行为。
func Handle(ctx context.Context, fn func() (bool, error), opts ...Option) error {
	if !funcutil.CheckCtxValid(ctx) {
		return ctx.Err()
	}

	log := log.Ctx(ctx)
	c := newDefaultConfig()

	for _, opt := range opts {
		opt(c)
	}

	var lastErr error
	for i := uint(0); i < c.attempts; i++ {
		if shouldRetry, err := fn(); err != nil {
			if i%4 == 0 {
				log.Warn("retry func failed",
					zap.Uint("retried", i),
					zap.String("caller", getCaller(2)),
					zap.Error(err),
				)
			}

			if !shouldRetry {
				isContextErr := errors.IsAny(err, context.Canceled, context.DeadlineExceeded)
				log.Warn("retry func failed, not be recoverable",
					zap.Uint("retried", i),
					zap.Uint("attempt", c.attempts),
					zap.Bool("isContextErr", isContextErr),
					zap.String("caller", getCaller(2)),
				)
				if isContextErr && lastErr != nil {
					return lastErr
				}
				return err
			}

			deadline, ok := ctx.Deadline()
			if ok && time.Until(deadline) < c.sleep {
				isContextErr := errors.IsAny(err, context.Canceled, context.DeadlineExceeded)
				log.Warn("retry func failed, deadline",
					zap.Uint("retried", i),
					zap.Uint("attempt", c.attempts),
					zap.Bool("isContextErr", isContextErr),
					zap.String("caller", getCaller(2)),
				)
				if isContextErr && lastErr != nil {
					return lastErr
				}
				return err
			}

			lastErr = err

			select {
			case <-time.After(c.sleep):
			case <-ctx.Done():
				log.Warn("retry func failed, ctx done",
					zap.Uint("retried", i),
					zap.Uint("attempt", c.attempts),
					zap.String("caller", getCaller(2)),
				)
				return lastErr
			}

			c.sleep *= 2
			if c.sleep > c.maxSleepTime {
				c.sleep = c.maxSleepTime
			}
		} else {
			return nil
		}
	}
	if lastErr != nil {
		log.Warn("retry func failed, reach max retry",
			zap.Uint("attempt", c.attempts),
			zap.String("caller", getCaller(2)),
		)
	}
	return lastErr
}

// errUnrecoverable 表示不可恢复错误的标记实例。
var errUnrecoverable = errors.New("unrecoverable error")

// Unrecoverable 将错误包装为不可恢复错误，使重试逻辑能够快速返回。
func Unrecoverable(err error) error {
	return merr.Combine(err, errUnrecoverable)
}

// IsRecoverable 判断给定错误是否为“可恢复”错误。
func IsRecoverable(err error) bool {
	return !errors.Is(err, errUnrecoverable)
}

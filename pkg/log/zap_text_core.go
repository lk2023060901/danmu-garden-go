// Copyright 2019 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

package log

import (
	"fmt"

	"go.uber.org/zap/zapcore"
)

// NewTextCore 创建一个将日志写入指定 WriteSyncer 的 Core。
func NewTextCore(enc zapcore.Encoder, ws zapcore.WriteSyncer, enab zapcore.LevelEnabler) zapcore.Core {
	return &textIOCore{
		LevelEnabler: enab,
		enc:          enc,
		out:          ws,
	}
}

// textIOCore 是 zapcore.ioCore 的简化拷贝，仅接受 *textEncoder。
// 在 https://github.com/uber-go/zap/pull/685 合并后可以考虑移除。
type textIOCore struct {
	zapcore.LevelEnabler
	enc zapcore.Encoder
	out zapcore.WriteSyncer
}

func (c *textIOCore) With(fields []zapcore.Field) zapcore.Core {
	clone := c.clone()
	// 与 zapcore.ioCore 不同，这里调用 textEncoder#addFields，
	// 用于修复 https://github.com/pingcap/log/issues/3 中的问题。
	switch e := clone.enc.(type) {
	case *textEncoder:
		e.addFields(fields)
	case zapcore.ObjectEncoder:
		for _, field := range fields {
			field.AddTo(e)
		}
	default:
		panic(fmt.Sprintf("unsupported encode type: %T for With operation", clone.enc))
	}
	return clone
}

func (c *textIOCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}

func (c *textIOCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	buf, err := c.enc.EncodeEntry(ent, fields)
	if err != nil {
		return err
	}
	_, err = c.out.Write(buf.Bytes())
	buf.Free()
	if err != nil {
		return err
	}
	if ent.Level > zapcore.ErrorLevel {
		// 有可能即将导致程序崩溃，此处强制同步输出。
		// Sync 错误暂时忽略，后续可参考 https://github.com/uber-go/zap/issues/370 进行完善。
		c.Sync()
	}
	return nil
}

func (c *textIOCore) Sync() error {
	return c.out.Sync()
}

func (c *textIOCore) clone() *textIOCore {
	return &textIOCore{
		LevelEnabler: c.LevelEnabler,
		enc:          c.enc.Clone(),
		out:          c.out,
	}
}

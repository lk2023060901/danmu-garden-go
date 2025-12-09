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
	"fmt"
	"time"

	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"

	"github.com/lk2023060901/danmu-garden-go/pkg/metrics"
	"github.com/lk2023060901/danmu-garden-go/pkg/util/syncutil"
)

var _ zapcore.Core = (*asyncTextIOCore)(nil)

// NewAsyncTextIOCore 创建一个异步文本 IO Core。
func NewAsyncTextIOCore(cfg *Config, ws zapcore.WriteSyncer, enab zapcore.LevelEnabler) *asyncTextIOCore {
	enc := newZapTextEncoder(cfg)
	bws := &zapcore.BufferedWriteSyncer{
		WS:            ws,
		Size:          cfg.AsyncWriteBufferSize,
		FlushInterval: cfg.AsyncWriteFlushInterval,
	}
	nonDroppableLevel, _ := zapcore.ParseLevel(cfg.AsyncWriteNonDroppableLevel)
	asyncTextIOCore := &asyncTextIOCore{
		LevelEnabler:        enab,
		notifier:            syncutil.NewAsyncTaskNotifier[struct{}](),
		enc:                 enc,
		bws:                 bws,
		pending:             make(chan *entryItem, cfg.AsyncWritePendingLength),
		writeDroppedTimeout: cfg.AsyncWriteDroppedTimeout,
		nonDroppableLevel:   nonDroppableLevel,
		stopTimeout:         cfg.AsyncWriteStopTimeout,
		maxBytesPerLog:      cfg.AsyncWriteMaxBytesPerLog,
	}
	go asyncTextIOCore.background()
	return asyncTextIOCore
}

// asyncTextIOCore 是对 textIOCore 的包装，通过带缓冲的 WriteSyncer 异步写入日志。
type asyncTextIOCore struct {
	zapcore.LevelEnabler

	notifier            *syncutil.AsyncTaskNotifier[struct{}]
	enc                 zapcore.Encoder
	bws                 *zapcore.BufferedWriteSyncer
	pending             chan *entryItem // 新进入的写请求队列。
	writeDroppedTimeout time.Duration
	nonDroppableLevel   zapcore.Level
	stopTimeout         time.Duration
	maxBytesPerLog      int
}

// entryItem 表示待写入底层缓冲 WriteSyncer 的日志条目。
type entryItem struct {
	buf   *buffer.Buffer
	level zapcore.Level
}

// With 返回一个携带额外字段的 Core 副本。
func (s *asyncTextIOCore) With(fields []zapcore.Field) zapcore.Core {
	enc := s.enc.Clone()
	switch e := enc.(type) {
	case *textEncoder:
		e.addFields(fields)
	case zapcore.ObjectEncoder:
		for _, field := range fields {
			field.AddTo(e)
		}
	default:
		panic(fmt.Sprintf("unsupported encode type: %T for With operation", enc))
	}
	return &asyncTextIOCore{
		LevelEnabler:        s.LevelEnabler,
		notifier:            s.notifier,
		enc:                 enc.Clone(),
		bws:                 s.bws,
		pending:             s.pending,
		writeDroppedTimeout: s.writeDroppedTimeout,
		stopTimeout:         s.stopTimeout,
		maxBytesPerLog:      s.maxBytesPerLog,
	}
}

// Check 检查当前日志条目是否满足 LevelEnabler 要求。
func (s *asyncTextIOCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if s.Enabled(ent.Level) {
		return ce.AddCore(ent, s)
	}
	return ce
}

// Write 将日志编码后写入异步缓冲队列。
// asyncTextIOCore 不保证写操作立即完成；当缓冲区已满或底层写入阻塞时，写操作可能被丢弃。
func (s *asyncTextIOCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	buf, err := s.enc.EncodeEntry(ent, fields)
	if err != nil {
		return err
	}
	entry := &entryItem{
		buf:   buf,
		level: ent.Level,
	}

	length := buf.Len()
	if length == 0 {
		return nil
	}
	var writeDroppedTimeout <-chan time.Time
	if ent.Level < s.nonDroppableLevel {
		writeDroppedTimeout = time.After(s.writeDroppedTimeout)
	}
	select {
	case s.pending <- entry:
		metrics.LoggingPendingWriteLength.Inc()
		metrics.LoggingPendingWriteBytes.Add(float64(length))
	case <-writeDroppedTimeout:
		metrics.LoggingDroppedWrites.Inc()
		// drop the entry if the write is dropped due to timeout
		buf.Free()
	}
	return nil
}

// Sync 为兼容接口，异步 Core 不执行额外同步。
func (s *asyncTextIOCore) Sync() error {
	return nil
}

// background 为后台协程，从 pending 队列中消费日志并写入底层缓冲 WriteSyncer。
func (s *asyncTextIOCore) background() {
	defer func() {
		s.flushPendingWriteWithTimeout()
		s.notifier.Finish(struct{}{})
	}()

	for {
		select {
		case <-s.notifier.Context().Done():
			return
		case ent := <-s.pending:
			s.consumeEntry(ent)
		}
	}
}

// consumeEntry 将单条日志写入底层缓冲 WriteSyncer，并更新指标与释放缓冲。
func (s *asyncTextIOCore) consumeEntry(ent *entryItem) {
	length := ent.buf.Len()
	metrics.LoggingPendingWriteLength.Dec()
	metrics.LoggingPendingWriteBytes.Sub(float64(length))
	writes := s.getWriteBytes(ent)
	if _, err := s.bws.Write(writes); err != nil {
		metrics.LoggingIOFailure.Inc()
	}
	ent.buf.Free()
	if ent.level > zapcore.ErrorLevel {
		s.bws.Sync()
	}
}

// getWriteBytes 计算写入底层缓冲 WriteSyncer 的字节切片。
// 若写入长度超过单条日志的最大限制，则会截断并返回截断后的字节。
func (s *asyncTextIOCore) getWriteBytes(ent *entryItem) []byte {
	length := ent.buf.Len()
	writes := ent.buf.Bytes()

	if length > s.maxBytesPerLog {
		// truncate the write if it exceeds the max bytes per log
		metrics.LoggingTruncatedWrites.Inc()
		metrics.LoggingTruncatedWriteBytes.Add(float64(length - s.maxBytesPerLog))

		end := writes[length-1]
		writes = writes[:s.maxBytesPerLog]
		writes[len(writes)-1] = end
	}
	return writes
}

// flushPendingWriteWithTimeout flushes the pending write with a timeout.
func (s *asyncTextIOCore) flushPendingWriteWithTimeout() {
	done := make(chan struct{})
	go s.flushAllPendingWrites(done)

	select {
	case <-time.After(s.stopTimeout):
	case <-done:
	}
}

// flushAllPendingWrites flushes all the pending writes to the underlying buffered write syncer.
func (s *asyncTextIOCore) flushAllPendingWrites(done chan struct{}) {
	defer func() {
		if err := s.bws.Stop(); err != nil {
			metrics.LoggingIOFailure.Inc()
		}
		close(done)
	}()

	for {
		select {
		case ent := <-s.pending:
			s.consumeEntry(ent)
		default:
			return
		}
	}
}

func (s *asyncTextIOCore) Stop() {
	s.notifier.Cancel()
	s.notifier.BlockUntilFinish()
}

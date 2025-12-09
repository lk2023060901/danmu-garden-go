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
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	defaultLogMaxSize = 300 // 日志文件默认最大大小，单位 MB。
)

// FileLogConfig 用于序列化文件日志相关配置（toml/json）。
type FileLogConfig struct {
	// RootPath 为日志文件根目录。
	RootPath string `toml:"rootpath" json:"rootpath"`
	// Filename 为日志文件名，留空表示关闭文件日志。
	Filename string `toml:"filename" json:"filename"`
	// MaxSize 表示单个日志文件的最大大小，单位 MB。
	MaxSize int `toml:"max-size" json:"max-size"`
	// MaxDays 表示日志文件最大保留天数，默认为不删除。
	MaxDays int `toml:"max-days" json:"max-days"`
	// MaxBackups 表示最多保留多少个历史日志文件。
	MaxBackups int `toml:"max-backups" json:"max-backups"`
}

// Config 用于序列化日志相关配置（toml/json）。
type Config struct {
	// Level 为日志级别。
	Level string `toml:"level" json:"level"`
	// GrpcLevel 为 gRPC 日志级别。
	GrpcLevel string `toml:"grpc-level" json:"grpc-level"`
	// Format 为日志格式，可选 json、text 或 console。
	Format string `toml:"format" json:"format"`
	// DisableTimestamp 表示是否禁用日志中的自动时间戳。
	DisableTimestamp bool `toml:"disable-timestamp" json:"disable-timestamp"`
	// Stdout 表示是否输出到标准输出。
	Stdout bool `toml:"stdout" json:"stdout"`
	// File 为文件日志配置。
	File FileLogConfig `toml:"file" json:"file"`
	// Development 为 true 时，日志处于开发模式，DPanicLevel 行为会变化，并更积极地输出堆栈信息。
	Development bool `toml:"development" json:"development"`
	// DisableCaller 表示是否关闭调用方文件名和行号标注，默认会标注。
	DisableCaller bool `toml:"disable-caller" json:"disable-caller"`
	// DisableStacktrace 表示是否完全关闭自动堆栈采集。
	// 默认在开发环境对 Warn 及以上等级记录堆栈，在生产环境对 Error 及以上等级记录堆栈。
	DisableStacktrace bool `toml:"disable-stacktrace" json:"disable-stacktrace"`
	// DisableErrorVerbose 表示是否关闭错误的详细信息输出。
	DisableErrorVerbose bool `toml:"disable-error-verbose" json:"disable-error-verbose"`
	// Sampling 为日志采样配置，用于限制日志对 CPU 和 I/O 的整体开销，同时尽量保留具有代表性的日志。
	// 这里的配置以“每秒”为单位，具体行为参考 zapcore.NewSampler。
	Sampling *zap.SamplingConfig `toml:"sampling" json:"sampling"`

	// AsyncWriteEnable 表示是否开启异步写日志。
	AsyncWriteEnable bool `toml:"async-write-enable" json:"async-write-enable"`

	// AsyncWriteFlushInterval 为异步写日志的刷新间隔。
	AsyncWriteFlushInterval time.Duration `toml:"async-write-flush-interval" json:"async-write-flush-interval"`

	// AsyncWriteDroppedTimeout 为缓冲区已满时丢弃写入请求前的超时时间。
	AsyncWriteDroppedTimeout time.Duration `toml:"async-write-dropped-timeout" json:"async-write-dropped-timeout"`

	// AsyncWriteNonDroppableLevel 表示在缓冲区已满时仍然不会被丢弃的最低日志级别。
	AsyncWriteNonDroppableLevel string `toml:"async-write-non-droppable-level" json:"async-write-non-droppable-level"`

	// AsyncWriteStopTimeout 为停止异步写入时的超时时间。
	AsyncWriteStopTimeout time.Duration `toml:"async-write-stop-timeout" json:"async-write-stop-timeout"`

	// AsyncWritePendingLength 为等待写入的最大请求数，超过部分的日志操作将被丢弃。
	AsyncWritePendingLength int `toml:"async-write-pending-length" json:"async-write-pending-length"`

	// AsyncWriteBufferSize 为写入缓冲区大小。
	AsyncWriteBufferSize int `toml:"async-write-buffer-size" json:"async-write-buffer-size"`

	// AsyncWriteMaxBytesPerLog 为单条日志允许的最大字节数。
	AsyncWriteMaxBytesPerLog int `toml:"async-write-max-bytes-per-log" json:"async-write-max-bytes-per-log"`
}

// ZapProperties 记录 zap 日志相关的核心信息。
type ZapProperties struct {
	Core   zapcore.Core
	Syncer zapcore.WriteSyncer
	Level  zap.AtomicLevel
}

func newZapTextEncoder(cfg *Config) zapcore.Encoder {
	return NewTextEncoderByConfig(cfg)
}

func (cfg *Config) buildOptions(errSink zapcore.WriteSyncer) []zap.Option {
	opts := []zap.Option{zap.ErrorOutput(errSink)}

	if cfg.Development {
		opts = append(opts, zap.Development())
	}

	if !cfg.DisableCaller {
		opts = append(opts, zap.AddCaller())
	}

	stackLevel := zap.ErrorLevel
	if cfg.Development {
		stackLevel = zap.WarnLevel
	}
	if !cfg.DisableStacktrace {
		opts = append(opts, zap.AddStacktrace(stackLevel))
	}

	if cfg.Sampling != nil {
		opts = append(opts, zap.WrapCore(func(core zapcore.Core) zapcore.Core {
			return zapcore.NewSamplerWithOptions(core, time.Second, cfg.Sampling.Initial, cfg.Sampling.Thereafter, zapcore.SamplerHook(cfg.Sampling.Hook))
		}))
	}
	return opts
}

// initialize 为 Config 填充缺省配置。
func (cfg *Config) initialize() {
	if cfg.AsyncWriteFlushInterval <= 0 {
		cfg.AsyncWriteFlushInterval = 10 * time.Second
	}
	if cfg.AsyncWriteDroppedTimeout <= 0 {
		cfg.AsyncWriteDroppedTimeout = 100 * time.Millisecond
	}
	if _, err := zapcore.ParseLevel(cfg.AsyncWriteNonDroppableLevel); cfg.AsyncWriteNonDroppableLevel == "" || err != nil {
		cfg.AsyncWriteNonDroppableLevel = zapcore.ErrorLevel.String()
	}
	if cfg.AsyncWriteStopTimeout <= 0 {
		cfg.AsyncWriteStopTimeout = 1 * time.Second
	}
	if cfg.AsyncWritePendingLength <= 0 {
		cfg.AsyncWritePendingLength = 1024
	}
	if cfg.AsyncWriteBufferSize <= 0 {
		cfg.AsyncWriteBufferSize = 4 * 1024
	}
	if cfg.AsyncWriteMaxBytesPerLog <= 0 {
		cfg.AsyncWriteMaxBytesPerLog = 1024 * 1024
	}
}

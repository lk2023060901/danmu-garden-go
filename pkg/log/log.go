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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/cockroachdb/errors"
	"github.com/uber/jaeger-client-go/utils"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"gopkg.in/natefinch/lumberjack.v2"
)

var _globalL, _globalP, _globalS, _globalR, _globalCleanup atomic.Value

var (
	_globalLevelLogger  sync.Map
	_namedRateLimiters  sync.Map
	_rateLimiterEnabled atomic.Bool
)

// RateLimiter 是限流日志帮助函数所需的最小接口。
type RateLimiter interface {
	CheckCredit(delta float64) bool
}

// nopRateLimiter 永远不会丢弃日志。
type nopRateLimiter struct{}

func (nopRateLimiter) CheckCredit(delta float64) bool { return true }

func init() {
	l, p := newStdLogger()

	replaceLeveledLoggers(l)
	_globalL.Store(l)
	_globalP.Store(p)

	s := _globalL.Load().(*zap.Logger).Sugar()
	_globalS.Store(s)

	// 初始化全局限流器；默认使用 utils.ReconfigurableRateLimiter，
	// 但默认关闭限流功能，由 R() 决定是否使用。
	rl := utils.NewRateLimiter(1.0, 60.0)
	_globalR.Store(rl)
	_rateLimiterEnabled.Store(false)
	configureRateLimiterFromEnv()
}

// InitLogger 初始化一个 zap 日志器。
func InitLogger(cfg *Config, opts ...zap.Option) (*zap.Logger, *ZapProperties, error) {
	var outputs []zapcore.WriteSyncer
	if len(cfg.File.Filename) > 0 {
		lg, err := initFileLog(&cfg.File)
		if err != nil {
			return nil, nil, err
		}
		outputs = append(outputs, zapcore.AddSync(lg))
	}
	if cfg.Stdout {
		stdOut, _, err := zap.Open([]string{"stdout"}...)
		if err != nil {
			return nil, nil, err
		}
		outputs = append(outputs, stdOut)
	}
	debugCfg := *cfg
	debugCfg.Level = "debug"
	outputsWriter := zap.CombineWriteSyncers(outputs...)
	debugL, r, err := InitLoggerWithWriteSyncer(&debugCfg, outputsWriter, opts...)
	if err != nil {
		return nil, nil, err
	}
	replaceLeveledLoggers(debugL)
	level := zapcore.DebugLevel
	parsedLevel := cfg.Level
	if strings.EqualFold(parsedLevel, "trace") {
		parsedLevel = "debug"
	}
	if err := level.UnmarshalText([]byte(parsedLevel)); err != nil {
		return nil, nil, err
	}
	r.Level.SetLevel(level)
	return debugL.WithOptions(zap.AddCallerSkip(1)), r, nil
}

// InitTestLogger 为单元测试初始化日志器。
func InitTestLogger(t zaptest.TestingT, cfg *Config, opts ...zap.Option) (*zap.Logger, *ZapProperties, error) {
	writer := newTestingWriter(t)
	zapOptions := []zap.Option{
		// Send zap errors to the same writer and mark the test as failed if
		// that happens.
		zap.ErrorOutput(writer.WithMarkFailed(true)),
	}
	opts = append(zapOptions, opts...)
	return InitLoggerWithWriteSyncer(cfg, writer, opts...)
}

// InitLoggerWithWriteSyncer 使用指定的 WriteSyncer 初始化 zap 日志器。
func InitLoggerWithWriteSyncer(cfg *Config, output zapcore.WriteSyncer, opts ...zap.Option) (*zap.Logger, *ZapProperties, error) {
	level := zap.NewAtomicLevel()
	err := level.UnmarshalText([]byte(cfg.Level))
	if err != nil {
		return nil, nil, fmt.Errorf("initLoggerWithWriteSyncer UnmarshalText cfg.Level err:%w", err)
	}
	var core zapcore.Core
	if cfg.AsyncWriteEnable {
		core = NewAsyncTextIOCore(cfg, output, level)
		registerCleanup(core.(*asyncTextIOCore).Stop)
	} else {
		core = NewTextCore(newZapTextEncoder(cfg), output, level)
	}
	opts = append(cfg.buildOptions(output), opts...)
	lg := zap.New(core, opts...)
	r := &ZapProperties{
		Core:   core,
		Syncer: output,
		Level:  level,
	}
	return lg, r, nil
}

// initFileLog 初始化基于文件的日志输出配置。
func initFileLog(cfg *FileLogConfig) (*lumberjack.Logger, error) {
	logPath := strings.Join([]string{cfg.RootPath, cfg.Filename}, string(filepath.Separator))
	if st, err := os.Stat(logPath); err == nil {
		if st.IsDir() {
			return nil, errors.New("can't use directory as log file name")
		}
	}
	if cfg.MaxSize == 0 {
		cfg.MaxSize = defaultLogMaxSize
	}

	// 使用 lumberjack 进行日志滚动。
	return &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    cfg.MaxSize,
		MaxBackups: cfg.MaxBackups,
		MaxAge:     cfg.MaxDays,
		LocalTime:  true,
	}, nil
}

func newStdLogger() (*zap.Logger, *ZapProperties) {
	conf := &Config{Level: "debug", Stdout: true, DisableErrorVerbose: true}
	lg, r, _ := InitLogger(conf, zap.OnFatal(zapcore.WriteThenPanic))
	return lg, r
}

// L 返回全局 Logger，可通过 ReplaceGlobals 重新配置。
// 该函数是并发安全的。
func L() *zap.Logger {
	return _globalL.Load().(*zap.Logger)
}

// S 返回全局 SugaredLogger，可通过 ReplaceGlobals 重新配置。
// 该函数是并发安全的。
func S() *zap.SugaredLogger {
	return _globalS.Load().(*zap.SugaredLogger)
}

// R 返回用于限流日志的全局 RateLimiter。
// 始终返回一个合法的限流器；当限流功能被关闭时，会回退到永不丢日志的 nop 实现。
func R() RateLimiter {
	// 未启用限流时，直接返回 nop 实现，避免引入额外的性能开销。
	if !_rateLimiterEnabled.Load() {
		return nopRateLimiter{}
	}

	val := _globalR.Load()
	if rl, ok := val.(*utils.ReconfigurableRateLimiter); ok && rl != nil {
		return rl
	}
	return nopRateLimiter{}
}

func ctxL() *zap.Logger {
	level := _globalP.Load().(*ZapProperties).Level.Level()
	l, ok := _globalLevelLogger.Load(level)
	if !ok {
		return L()
	}
	return l.(*zap.Logger)
}

func debugL() *zap.Logger {
	v, _ := _globalLevelLogger.Load(zapcore.DebugLevel)
	return v.(*zap.Logger)
}

func infoL() *zap.Logger {
	v, _ := _globalLevelLogger.Load(zapcore.InfoLevel)
	return v.(*zap.Logger)
}

func warnL() *zap.Logger {
	v, _ := _globalLevelLogger.Load(zapcore.WarnLevel)
	return v.(*zap.Logger)
}

func errorL() *zap.Logger {
	v, _ := _globalLevelLogger.Load(zapcore.ErrorLevel)
	return v.(*zap.Logger)
}

func fatalL() *zap.Logger {
	v, _ := _globalLevelLogger.Load(zapcore.FatalLevel)
	return v.(*zap.Logger)
}

// Cleanup 清理全局 Logger 和 SugaredLogger。
func Cleanup() {
	cleanup := _globalCleanup.Load()
	if cleanup != nil {
		cleanup.(func())()
	}
}

// ReplaceGlobals 替换全局 Logger 和 SugaredLogger。
// 该函数是并发安全的。
func ReplaceGlobals(logger *zap.Logger, props *ZapProperties) {
	_globalL.Store(logger)
	_globalS.Store(logger.Sugar())
	_globalP.Store(props)
}

// registerCleanup 注册全局日志清理时需要执行的清理函数。
func registerCleanup(cleanup func()) {
	oldCleanup := _globalCleanup.Swap(cleanup)
	if oldCleanup != nil {
		oldCleanup.(func())()
	}
}

func replaceLeveledLoggers(debugLogger *zap.Logger) {
	levels := []zapcore.Level{
		zapcore.DebugLevel, zapcore.InfoLevel, zapcore.WarnLevel, zapcore.ErrorLevel,
		zapcore.DPanicLevel, zapcore.PanicLevel, zapcore.FatalLevel,
	}
	for _, level := range levels {
		levelL := debugLogger.WithOptions(zap.IncreaseLevel(level))
		_globalLevelLogger.Store(level, levelL)
	}
}

// Sync flushes any buffered log entries.
func Sync() error {
	if err := L().Sync(); err != nil {
		return err
	}
	if err := S().Sync(); err != nil {
		return err
	}
	var reterr error
	_globalLevelLogger.Range(func(key, val interface{}) bool {
		l := val.(*zap.Logger)
		if err := l.Sync(); err != nil {
			reterr = err
			return false
		}
		return true
	})
	return reterr
}

func Level() zap.AtomicLevel {
	return _globalP.Load().(*ZapProperties).Level
}

// configureRateLimiterFromEnv 按 ZEUS_LOG_RATE_* 环境变量配置全局限流器。
//
//   - ZEUS_LOG_RATE_ENABLE: "1"/"true" 启用限流（默认关闭）。
//   - ZEUS_LOG_RATE_CREDIT_PER_SECOND: float，默认 1.0。
//   - ZEUS_LOG_RATE_MAX_BALANCE: float，默认 60.0。
func configureRateLimiterFromEnv() {
	// 默认关闭限流，只有显式配置 ZEUS_LOG_RATE_ENABLE 时才启用。
	enabled := getenvBool("ZEUS_LOG_RATE_ENABLE", false)
	_rateLimiterEnabled.Store(enabled)

	// 无需配置具体参数，直接视为禁用限流。
	if !enabled {
		return
	}

	credit := getenvFloat("ZEUS_LOG_RATE_CREDIT_PER_SECOND", 1.0)
	maxBalance := getenvFloat("ZEUS_LOG_RATE_MAX_BALANCE", 60.0)

	val := _globalR.Load()
	rl, ok := val.(*utils.ReconfigurableRateLimiter)
	if !ok || rl == nil {
		rl = utils.NewRateLimiter(credit, maxBalance)
		_globalR.Store(rl)
	} else {
		rl.Update(credit, maxBalance)
	}
}

func getenvDefault(key, def string) string {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	return val
}

func getenvBool(key string, def bool) bool {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	switch strings.ToLower(val) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

func getenvFloat(key string, def float64) float64 {
	val := getenvDefault(key, "")
	if val == "" {
		return def
	}
	f, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return def
	}
	return f
}

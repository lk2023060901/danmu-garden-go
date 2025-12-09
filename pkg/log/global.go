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
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type ctxLogKeyType struct{}

var CtxLogKey = ctxLogKeyType{}

// Debug 在 Debug 级别输出一条日志。
// 消息包含调用处传入的字段以及 Logger 已经携带的字段。
// Deprecated: 请使用 Ctx(ctx).Debug 代替。
func Debug(msg string, fields ...zap.Field) {
	L().Debug(msg, fields...)
}

// Info 在 Info 级别输出一条日志。
// 消息包含调用处传入的字段以及 Logger 已经携带的字段。
// Deprecated: 请使用 Ctx(ctx).Info 代替。
func Info(msg string, fields ...zap.Field) {
	L().Info(msg, fields...)
}

// Warn 在 Warn 级别输出一条日志。
// 消息包含调用处传入的字段以及 Logger 已经携带的字段。
// Deprecated: 请使用 Ctx(ctx).Warn 代替。
func Warn(msg string, fields ...zap.Field) {
	L().Warn(msg, fields...)
}

// Error 在 Error 级别输出一条日志。
// 消息包含调用处传入的字段以及 Logger 已经携带的字段。
// Deprecated: 请使用 Ctx(ctx).Error 代替。
func Error(msg string, fields ...zap.Field) {
	L().Error(msg, fields...)
}

// Panic 在 Panic 级别输出一条日志。
// 消息包含调用处传入的字段以及 Logger 已经携带的字段。
//
// 无论是否开启 Panic 级别日志，Logger 都会在记录后直接触发 panic。
// Deprecated: 请使用 Ctx(ctx).Panic 代替。
func Panic(msg string, fields ...zap.Field) {
	L().Panic(msg, fields...)
}

// Fatal 在 Fatal 级别输出一条日志。
// 消息包含调用处传入的字段以及 Logger 已经携带的字段。
//
// 无论是否开启 Fatal 级别日志，Logger 都会在记录后调用 os.Exit(1) 退出进程。
// Deprecated: 请使用 Ctx(ctx).Fatal 代替。
func Fatal(msg string, fields ...zap.Field) {
	L().Fatal(msg, fields...)
}

// RatedDebug 以 Debug 级别输出限流日志。
// 通过限流器控制日志打印频率，避免产生过多日志。
// 返回值为 true 表示本次日志已成功输出。
// Deprecated: 请使用 Ctx(ctx).RatedDebug 代替。
func RatedDebug(cost float64, msg string, fields ...zap.Field) bool {
	if R().CheckCredit(cost) {
		L().Debug(msg, fields...)
		return true
	}
	return false
}

// RatedInfo 以 Info 级别输出限流日志。
// 通过限流器控制日志打印频率，避免产生过多日志。
// 返回值为 true 表示本次日志已成功输出。
// Deprecated: 请使用 Ctx(ctx).RatedInfo 代替。
func RatedInfo(cost float64, msg string, fields ...zap.Field) bool {
	if R().CheckCredit(cost) {
		L().Info(msg, fields...)
		return true
	}
	return false
}

// RatedWarn 以 Warn 级别输出限流日志。
// 通过限流器控制日志打印频率，避免产生过多日志。
// 返回值为 true 表示本次日志已成功输出。
// Deprecated: 请使用 Ctx(ctx).RatedWarn 代替。
func RatedWarn(cost float64, msg string, fields ...zap.Field) bool {
	if R().CheckCredit(cost) {
		L().Warn(msg, fields...)
		return true
	}
	return false
}

// With 创建一个携带额外字段的子 Logger。
// 子 Logger 添加的字段不会影响父 Logger，反之亦然。
// Deprecated: 请使用 Ctx(ctx).With 代替。
func With(fields ...zap.Field) *MLogger {
	return &MLogger{
		Logger: L().WithOptions(zap.WrapCore(func(core zapcore.Core) zapcore.Core {
			return NewLazyWith(core, fields)
		})).WithOptions(zap.AddCallerSkip(-1)),
	}
}

// SetLevel 设置全局日志级别。
func SetLevel(l zapcore.Level) {
	_globalP.Load().(*ZapProperties).Level.SetLevel(l)
}

// GetLevel 获取当前全局日志级别。
func GetLevel() zapcore.Level {
	return _globalP.Load().(*ZapProperties).Level.Level()
}

// WithTraceID 返回一个携带 trace_id 字段的上下文。
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return WithFields(ctx, zap.String("traceID", traceID))
}

// WithReqID 为 ctx 中的 Logger 添加 reqID 字段。
func WithReqID(ctx context.Context, reqID int64) context.Context {
	fields := []zap.Field{zap.Int64("reqID", reqID)}
	return WithFields(ctx, fields...)
}

// WithModule 为 ctx 中的 Logger 添加模块名字段。
func WithModule(ctx context.Context, module string) context.Context {
	fields := []zap.Field{zap.String(FieldNameModule, module)}
	return WithFields(ctx, fields...)
}

// WithFields 返回一个附加了指定字段的上下文。
func WithFields(ctx context.Context, fields ...zap.Field) context.Context {
	var zlogger *zap.Logger
	if ctxLogger, ok := ctx.Value(CtxLogKey).(*MLogger); ok {
		zlogger = ctxLogger.Logger
	} else {
		zlogger = ctxL()
	}
	mLogger := &MLogger{
		Logger: zlogger.With(fields...),
	}
	return context.WithValue(ctx, CtxLogKey, mLogger)
}

// NewIntentContext 创建一个携带意图信息的新上下文，并返回对应的 trace.Span。
func NewIntentContext(name string, intent string) (context.Context, trace.Span) {
	intentCtx, initSpan := otel.Tracer(name).Start(context.Background(), intent)
	intentCtx = WithFields(intentCtx,
		zap.String("role", name),
		zap.String("intent", intent),
		zap.String("traceID", initSpan.SpanContext().TraceID().String()))
	return intentCtx, initSpan
}

// Ctx 返回一个基于 ctx 附加字段输出日志的 Logger。
func Ctx(ctx context.Context) *MLogger {
	if ctx == nil {
		return &MLogger{Logger: ctxL()}
	}
	if ctxLogger, ok := ctx.Value(CtxLogKey).(*MLogger); ok {
		return ctxLogger
	}
	return &MLogger{Logger: ctxL()}
}

// withLogLevel 返回一个携带指定日志级别 Logger 的上下文。
// 注意：该函数会覆盖之前已经附加在 ctx 上的 Logger。
func withLogLevel(ctx context.Context, level zapcore.Level) context.Context {
	var zlogger *zap.Logger
	switch level {
	case zap.DebugLevel:
		zlogger = debugL()
	case zap.InfoLevel:
		zlogger = infoL()
	case zap.WarnLevel:
		zlogger = warnL()
	case zap.ErrorLevel:
		zlogger = errorL()
	case zap.FatalLevel:
		zlogger = fatalL()
	default:
		zlogger = L()
	}
	return context.WithValue(ctx, CtxLogKey, &MLogger{Logger: zlogger})
}

// WithDebugLevel 返回一个携带 Debug 级别 Logger 的上下文。
// 注意：会覆盖之前附加在 ctx 上的 Logger。
func WithDebugLevel(ctx context.Context) context.Context {
	return withLogLevel(ctx, zapcore.DebugLevel)
}

// WithInfoLevel 返回一个携带 Info 级别 Logger 的上下文。
// 注意：会覆盖之前附加在 ctx 上的 Logger。
func WithInfoLevel(ctx context.Context) context.Context {
	return withLogLevel(ctx, zapcore.InfoLevel)
}

// WithWarnLevel 返回一个携带 Warn 级别 Logger 的上下文。
// 注意：会覆盖之前附加在 ctx 上的 Logger。
func WithWarnLevel(ctx context.Context) context.Context {
	return withLogLevel(ctx, zapcore.WarnLevel)
}

// WithErrorLevel 返回一个携带 Error 级别 Logger 的上下文。
// 注意：会覆盖之前附加在 ctx 上的 Logger。
func WithErrorLevel(ctx context.Context) context.Context {
	return withLogLevel(ctx, zapcore.ErrorLevel)
}

// WithFatalLevel 返回一个携带 Fatal 级别 Logger 的上下文。
// 注意：会覆盖之前附加在 ctx 上的 Logger。
func WithFatalLevel(ctx context.Context) context.Context {
	return withLogLevel(ctx, zapcore.FatalLevel)
}

package logutil

import (
	"context"
	"strconv"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/lk2023060901/danmu-garden-go/pkg/log"
)

const (
	logLevelRPCMetaKeyLegacy = "log_level"
	logLevelRPCMetaKey       = "log-level"
	clientRequestIDKeyLegacy = "client-request-id"
	clientRequestIDKey       = "client_request_id"
)

// UnaryTraceLoggerInterceptor 在一元 RPC 调用的上下文中注入带 Trace 信息的 Logger。
func UnaryTraceLoggerInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	newctx := withLevelAndTrace(ctx)
	return handler(newctx, req)
}

// StreamTraceLoggerInterceptor 在流式 RPC 调用的上下文中注入带 Trace 信息的 Logger。
func StreamTraceLoggerInterceptor(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx := ss.Context()
	newctx := withLevelAndTrace(ctx)
	wrappedStream := grpc_middleware.WrapServerStream(ss)
	wrappedStream.WrappedContext = newctx
	return handler(srv, wrappedStream)
}

func withLevelAndTrace(ctx context.Context) context.Context {
	newctx := ctx
	var traceID trace.TraceID
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		levels := GetMetadata(md, logLevelRPCMetaKey, logLevelRPCMetaKeyLegacy)
		// 解析客户端传入的日志级别。
		if len(levels) >= 1 {
			level := zapcore.DebugLevel
			if err := level.UnmarshalText([]byte(levels[0])); err != nil {
				newctx = ctx
			} else {
				switch level {
				case zapcore.DebugLevel:
					newctx = log.WithDebugLevel(ctx)
				case zapcore.InfoLevel:
					newctx = log.WithInfoLevel(ctx)
				case zapcore.WarnLevel:
					newctx = log.WithWarnLevel(ctx)
				case zapcore.ErrorLevel:
					newctx = log.WithErrorLevel(ctx)
				case zapcore.FatalLevel:
					newctx = log.WithFatalLevel(ctx)
				default:
					newctx = ctx
				}
			}
			// 将日志级别写回到 outgoing metadata 中。
			newctx = metadata.AppendToOutgoingContext(newctx, logLevelRPCMetaKey, level.String())
		}
		// 客户端请求 ID。
		requestID := GetMetadata(md, clientRequestIDKey, clientRequestIDKeyLegacy)
		if len(requestID) >= 1 {
			// 将客户端请求 ID 注入到 outgoing metadata 中。
			newctx = metadata.AppendToOutgoingContext(newctx, clientRequestIDKey, requestID[0])
			var err error
			// 如果 client-request-id 是合法的 TraceID，则直接使用该 TraceID。
			traceID, err = trace.TraceIDFromHex(requestID[0])
			if err != nil {
				// 如果不是合法的 TraceID，则以普通字段形式记录请求 ID。
				newctx = log.WithFields(newctx, zap.String(clientRequestIDKey, requestID[0]))
			}
		}
	}
	// 解析客户端请求的时间戳（毫秒）。
	requestUnixmsec, ok := GetClientReqUnixmsecGrpc(newctx)
	if ok {
		newctx = log.WithFields(newctx, zap.Int64("clientRequestUnixmsec", requestUnixmsec))
	}

	// 如果当前 TraceID 不合法，则从上下文中生成/获取新的 TraceID。
	if !traceID.IsValid() {
		traceID = trace.SpanContextFromContext(newctx).TraceID()
	}
	if traceID.IsValid() {
		newctx = log.WithTraceID(newctx, traceID.String())
	}
	return newctx
}

func GetClientReqUnixmsecGrpc(ctx context.Context) (int64, bool) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return -1, false
	}

	requestUnixmsecs := GetMetadata(md, "client-request-msec")
	if len(requestUnixmsecs) < 1 {
		return -1, false
	}
	requestUnixmsec, err := strconv.ParseInt(requestUnixmsecs[0], 10, 64)
	if err != nil {
		return -1, false
	}
	return requestUnixmsec, true
}

func GetMetadata(md metadata.MD, keys ...string) []string {
	var result []string
	for _, key := range keys {
		if values := md.Get(key); len(values) > 0 {
			result = append(result, values...)
		}
	}
	return result
}

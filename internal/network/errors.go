package network

import "errors"

// Stage 表示网络收发链路中的处理阶段。
//
// 主要用于在回调中标记错误发生的位置，便于监控与排查。
type Stage string

const (
	StageHandshake Stage = "handshake"
	StageRecvRaw   Stage = "recv_raw"  // 收到底层原始字节（WebSocket 帧等）
	StageDecode    Stage = "decode"    // 原始字节 -> Packet
	StageDispatch  Stage = "dispatch"  // Packet -> 业务处理
	StageEncode    Stage = "encode"    // 业务对象 -> Packet/字节
	StageSend      Stage = "send"      // 底层发送完成
)

// 统一的错误码常量。
//
// 注意：这些是用于日志/监控的稳定字符串，真正的 error 对象在下面通过 errors.New 构造。
const (
	ErrCodeHandshakeFailed = "network:handshake_failed"
	ErrCodeRecvFailed      = "network:recv_failed"
	ErrCodeDecodeFailed    = "network:decode_failed"
	ErrCodeDispatchFailed  = "network:dispatch_failed"
	ErrCodeEncodeFailed    = "network:encode_failed"
	ErrCodeSendFailed      = "network:send_failed"
)

var (
	// ErrHandshakeFailed 表示握手阶段失败（例如 WebSocket 升级失败）。
	ErrHandshakeFailed = errors.New(ErrCodeHandshakeFailed)

	// ErrRecvFailed 表示在读取底层连接数据时发生错误。
	ErrRecvFailed = errors.New(ErrCodeRecvFailed)

	// ErrDecodeFailed 表示在将原始字节解码为 Packet 时发生错误。
	ErrDecodeFailed = errors.New(ErrCodeDecodeFailed)

	// ErrDispatchFailed 表示在将 Packet 分发给业务处理时发生错误。
	ErrDispatchFailed = errors.New(ErrCodeDispatchFailed)

	// ErrEncodeFailed 表示在将业务对象编码为 Packet 或字节时发生错误。
	ErrEncodeFailed = errors.New(ErrCodeEncodeFailed)

	// ErrSendFailed 表示在发送数据到对端时发生错误。
	ErrSendFailed = errors.New(ErrCodeSendFailed)
)


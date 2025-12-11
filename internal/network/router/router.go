package router

import (
	"fmt"

	"github.com/lk2023060901/danmu-garden-go/internal/network/serializer"
	"github.com/lk2023060901/danmu-garden-go/internal/network/session"
	protos "github.com/lk2023060901/danmu-garden-game-protos"
)

// Handler 是框架暴露给业务层的通用处理函数签名。
//
// 说明：
//   - sess：当前会话，用于关联玩家/用户，并发送响应；由框架层定义 Session 抽象；
//   - req ：已经经过反序列化的请求对象（通常为 *XXXRequest），
//           具体类型由业务侧在 Register 时通过 newReq 决定；
//   - 返回：
//       - resp：可选的响应对象（通常为 *XXXResponse），为 nil 时表示无需自动发送响应；
//       - err ：业务执行失败时的错误，由上层决定如何记录或转换为错误码。
type Handler func(sess session.Session, req any) (resp any, err error)

// Route 描述一条路由规则：请求协议号 -> 请求类型 + 业务 Handler + 响应协议号。
type Route struct {
	// NewRequest 用于创建一个空的请求对象实例。
	//
	// 要求：
	//   - 必须返回指向具体请求类型的指针（例如：func() any { return &pb.LoginRequest{} }）。
	NewRequest func() any

	// Handler 为业务层实现的处理函数。
	//
	// Router 会在反序列化完成后调用该函数，并根据其返回值决定是否发送响应。
	Handler Handler

	// RespOp 为响应消息使用的协议号。
	//
	// 说明：
	//   - 当 RespOp 为 0 时，Router 不会根据 Handler 返回值自动发送响应；
	//     这种情况下，业务可以在 Handler 内部自行调用 sess.Send 进行发送。
	//   - 当 RespOp 非 0 且 Handler 返回非 nil 的 resp 时，
	//     Router 会自动构造响应头并通过 sess.Send 发送。
	RespOp uint32
}

// Router 维护协议号到路由规则的映射，并负责从“原始帧”到业务 Handler 的完整调度流程。
//
// 典型调用链（服务器侧）：
//   1. Codec/Framer 从底层连接读取出 Envelope，得到 header + payload；
//   2. 上层调用 Router.Handle(sess, header, payload)；
//   3. Router 根据 header.Op 找到 Route：
//        - newReq() 创建请求对象；
//        - 使用 Serializer.Unmarshal(payload, req) 反序列化；
//        - 调用业务 Handler(sess, req)；
//        - 如有需要，构造响应头并调用 sess.Send 发送响应。
type Router interface {
	// Register 为协议号 op 注册一条路由规则。
	//
	// 参数：
	//   - op    ：请求协议号；
	//   - route：路由规则，其中 NewRequest/Handler/RespOp 均不能为空或非法。
	//
	// 要求：
	//   - 同一协议号不允许重复注册，重复时应返回错误。
	Register(op uint32, route Route) error

	// Handle 处理一条已经解析出的消息。
	//
	// 参数：
	//   - sess   ：当前会话，代表一条具体连接；
	//   - header ：MessageHeader，来自对端，包含 op/seq/flags/timestamp 等元数据；
	//   - payload：已完成解密/解压的业务层字节序列。
	//
	// 行为：
	//   1. 根据 header.Op 查找 Route；
	//   2. 调用 Route.NewRequest() 创建请求对象；
	//   3. 使用注入的 Serializer 将 payload 反序列化到请求对象；
	//   4. 调用业务 Handler(sess, req)；
	//   5. 若 Route.RespOp != 0 且 resp 非 nil，则构造响应头并通过 sess.Send 发送响应。
	Handle(sess session.Session, header *protos.MessageHeader, payload []byte) error
}

// defaultRouter 是 Router 接口的基础实现。
//
// 它基于一个简单的 map[op]Route 进行路由，使用注入的 Serializer 完成请求对象的编解码。
type defaultRouter struct {
	ser    serializer.Serializer
	routes map[uint32]Route
}

// 编译期断言：确保 defaultRouter 实现了 Router 接口。
var _ Router = (*defaultRouter)(nil)

// New 创建一个基于给定 Serializer 的 Router 实例。
func New(ser serializer.Serializer) Router {
	return &defaultRouter{
		ser:    ser,
		routes: make(map[uint32]Route),
	}
}

// Register 实现 Router.Register。
func (r *defaultRouter) Register(op uint32, route Route) error {
	if op == 0 {
		return fmt.Errorf("router: op must not be 0")
	}
	if route.NewRequest == nil {
		return fmt.Errorf("router: NewRequest is nil for op=%d", op)
	}
	if route.Handler == nil {
		return fmt.Errorf("router: Handler is nil for op=%d", op)
	}
	if _, exists := r.routes[op]; exists {
		return fmt.Errorf("router: op=%d already registered", op)
	}
	r.routes[op] = route
	return nil
}

// Handle 实现 Router.Handle。
func (r *defaultRouter) Handle(sess session.Session, header *protos.MessageHeader, payload []byte) error {
	if sess == nil {
		return fmt.Errorf("router: session is nil")
	}
	if header == nil {
		return fmt.Errorf("router: header is nil")
	}

	route, ok := r.routes[header.Op]
	if !ok {
		return fmt.Errorf("router: no handler for op=%d", header.Op)
	}

	// 1. 构造请求对象并反序列化。
	req := route.NewRequest()
	if req == nil {
		return fmt.Errorf("router: NewRequest returned nil for op=%d", header.Op)
	}
	if len(payload) > 0 {
		if err := r.ser.Unmarshal(payload, req); err != nil {
			return fmt.Errorf("router: unmarshal payload failed for op=%d: %w", header.Op, err)
		}
	}

	// 2. 调用业务 Handler。
	resp, err := route.Handler(sess, req)
	if err != nil {
		return err
	}

	// 3. 根据路由规则决定是否自动发送响应。
	if route.RespOp == 0 || resp == nil {
		return nil
	}

	if err := sess.Send(route.RespOp, resp); err != nil {
		return fmt.Errorf("router: send response failed for op=%d: %w", header.Op, err)
	}

	return nil
}

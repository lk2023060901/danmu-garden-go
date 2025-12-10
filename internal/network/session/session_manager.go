package session

// SessionManager 维护当前所有在线会话的索引。
//
// 职责说明：
//   - 只负责会话的注册、查询和移除，不直接创建或关闭底层连接；
//   - Session 的具体生命周期（何时创建/关闭）由上层的 acceptor/connector 决定；
//   - 业务层可以基于 SessionManager 实现广播、按 ID 定向发送等能力。
type SessionManager interface {
	// Register 将一个已创建好的 Session 注册到管理器中。
	//
	// 要求：
	//   - sess.ID() 必须是全局唯一的 64 位无符号整型；
	//   - 当存在相同 ID 的会话时，应返回错误，避免覆盖旧会话。
	Register(sess Session) error

	// Get 根据 session id 查找会话。
	//
	// 返回：
	//   - sess：找到的会话对象；
	//   - ok  ：true 表示找到，false 表示不存在。
	Get(id uint64) (sess Session, ok bool)

	// Unregister 从管理器中移除指定 id 的会话。
	//
	// 说明：
	//   - 仅删除索引，不负责调用 sess.Close()；
	//   - 一般在会话关闭后，由上层组件调用 Unregister 做清理。
	Unregister(id uint64) error

	// Range 遍历当前所有在线会话。
	//
	// 参数：
	//   - fn：回调函数，入参为每一个 Session；
	//         当 fn 返回 false 时，中断遍历。
	Range(fn func(sess Session) bool)

	// Count 返回当前已注册的会话数量。
	Count() int
}


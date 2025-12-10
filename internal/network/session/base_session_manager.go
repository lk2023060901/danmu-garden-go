package session

import (
	"fmt"
	"sync"
)

// BaseSessionManager 提供了基于内存 map 的 SessionManager 实现。
//
// 特性：
//   - 使用读写锁保证并发安全；
//   - Register 在遇到重复 ID 时返回错误，避免覆盖旧会话；
//   - Range 在遍历前复制一份会话切片，避免在持锁情况下执行用户回调。
type BaseSessionManager struct {
	mu       sync.RWMutex
	sessions map[uint64]Session
}

// 确保 BaseSessionManager 实现了 SessionManager 接口。
var _ SessionManager = (*BaseSessionManager)(nil)

// NewBaseSessionManager 创建一个空的 BaseSessionManager。
func NewBaseSessionManager() *BaseSessionManager {
	return &BaseSessionManager{
		sessions: make(map[uint64]Session),
	}
}

// Register 实现 SessionManager.Register。
func (m *BaseSessionManager) Register(sess Session) error {
	if sess == nil {
		return nil
	}
	id := sess.ID()

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sessions[id]; exists {
		return fmt.Errorf("session: id %d already registered", id)
	}
	m.sessions[id] = sess
	return nil
}

// Get 实现 SessionManager.Get。
func (m *BaseSessionManager) Get(id uint64) (Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sess, ok := m.sessions[id]
	return sess, ok
}

// Unregister 实现 SessionManager.Unregister。
func (m *BaseSessionManager) Unregister(id uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sessions[id]; !exists {
		return fmt.Errorf("session: id %d not found", id)
	}
	delete(m.sessions, id)
	return nil
}

// Range 实现 SessionManager.Range。
func (m *BaseSessionManager) Range(fn func(sess Session) bool) {
	if fn == nil {
		return
	}

	m.mu.RLock()
	snapshot := make([]Session, 0, len(m.sessions))
	for _, sess := range m.sessions {
		snapshot = append(snapshot, sess)
	}
	m.mu.RUnlock()

	for _, sess := range snapshot {
		if !fn(sess) {
			return
		}
	}
}

// Count 实现 SessionManager.Count。
func (m *BaseSessionManager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

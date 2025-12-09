package log

import "go.uber.org/atomic"

var (
	_ WithLogger   = &Binder{}
	_ LoggerBinder = &Binder{}
)

// WithLogger 是一个用于访问本地 Logger 的接口。
type WithLogger interface {
	Logger() *MLogger
}

// LoggerBinder 是一个用于设置 Logger 的接口。
type LoggerBinder interface {
	SetLogger(logger *MLogger)
}

// Binder 是一个嵌入式类型，用于在组件内部统一管理和访问 Logger。
type Binder struct {
	logger atomic.Pointer[MLogger]
}

// SetLogger 将 Logger 绑定到 Binder 上。
func (w *Binder) SetLogger(logger *MLogger) {
	w.logger.Store(logger)
}

// Logger 返回当前绑定在 Binder 上的 Logger。
// 如果尚未绑定，则退回到全局 Logger。
func (w *Binder) Logger() *MLogger {
	l := w.logger.Load()
	if l == nil {
		return With()
	}
	return l
}

package log

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	FieldNameModule    = "module"
	FieldNameComponent = "component"
)

// FieldModule 返回一个包含模块名的 zap 字段。
func FieldModule(module string) zap.Field {
	return zap.String(FieldNameModule, module)
}

// FieldComponent 返回一个包含组件名的 zap 字段。
func FieldComponent(component string) zap.Field {
	return zap.String(FieldNameComponent, component)
}

// FieldMessage 返回一个包含消息对象的 zap 字段。
func FieldMessage(msg zapcore.ObjectMarshaler) zap.Field {
	return zap.Object("message", msg)
}

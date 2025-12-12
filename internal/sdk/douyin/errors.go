package douyin

import "fmt"

// Error 表示由抖音 OpenAPI 返回的业务错误。
//
// 大多数接口会返回统一结构体，其中包含 err_no/err_tips 字段。
// SDK 将其包装为 Error 便于统一处理和上报。
type Error struct {
	ErrNo   int
	ErrTips string

	RawBody []byte
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.ErrTips != "" {
		return fmt.Sprintf("douyin: err_no=%d err_tips=%s", e.ErrNo, e.ErrTips)
	}
	return fmt.Sprintf("douyin: err_no=%d", e.ErrNo)
}

// IsAPICodeError 判断错误是否为 Douyin OpenAPI 返回的业务错误。
func IsAPICodeError(err error) (*Error, bool) {
	if err == nil {
		return nil, false
	}
	e, ok := err.(*Error)
	return e, ok
}

package serializer

import (
	"github.com/lk2023060901/danmu-garden-go/internal/json"
)

// JSONSerializer 使用 internal/json（基于 bytedance/sonic）实现 JSON 编解码。
type JSONSerializer struct{}

// 编译期断言：确保 JSONSerializer 实现了 Serializer 接口。
var _ Serializer = (*JSONSerializer)(nil)

func (JSONSerializer) Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func (JSONSerializer) Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

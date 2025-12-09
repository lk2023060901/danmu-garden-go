package serializer

import (
	"fmt"

	"google.golang.org/protobuf/proto"
)

// ProtoSerializer 使用 Protobuf 进行二进制序列化。
//
// 注意：传入/传出的对象必须实现 proto.Message。
type ProtoSerializer struct{}

// 编译期断言：确保 ProtoSerializer 实现了 Serializer 接口。
var _ Serializer = (*ProtoSerializer)(nil)

func (ProtoSerializer) Marshal(v any) ([]byte, error) {
	msg, ok := v.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("serializer: ProtoSerializer requires proto.Message, got %T", v)
	}
	return proto.Marshal(msg)
}

func (ProtoSerializer) Unmarshal(data []byte, v any) error {
	msg, ok := v.(proto.Message)
	if !ok {
		return fmt.Errorf("serializer: ProtoSerializer requires proto.Message, got %T", v)
	}
	return proto.Unmarshal(data, msg)
}

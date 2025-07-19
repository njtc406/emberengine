// Package codec
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/19 0019 18:52
// 最后更新:  yr  2025/7/19 0019 18:52
package codec

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"reflect"
	"sync"
)

var protoTypeCache sync.Map

func getProtoType(typeName string) (protoreflect.MessageType, error) {
	if val, ok := protoTypeCache.Load(typeName); ok {
		return val.(protoreflect.MessageType), nil
	}
	t, err := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName(typeName))
	if err != nil {
		return nil, err
	}
	protoTypeCache.Store(typeName, t)
	return t, nil
}

var protoTypeNameCache sync.Map // map[reflect.Type]string

func getProtoTypeName(m proto.Message) string {
	t := reflect.TypeOf(m)
	if v, ok := protoTypeNameCache.Load(t); ok {
		return v.(string)
	}
	name := string(proto.MessageName(m))
	protoTypeNameCache.Store(t, name)
	return name
}

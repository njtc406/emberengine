// Package codec
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/19 0019 19:29
// 最后更新:  yr  2025/7/19 0019 19:29
package codec

import (
	"fmt"

	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

var codecs = map[int32]inf.ICodec{}

func RegisterCodec(coder inf.ICodec) {
	codecs[coder.Type()] = coder
}

func GetCodec(typ int32) (inf.ICodec, error) {
	c, ok := codecs[typ]
	if !ok {
		return nil, fmt.Errorf("unknown codec type: %d", typ)
	}
	return c, nil
}

func Encode(typ int32, msg interface{}) ([]byte, error) {
	coder, err := GetCodec(typ)
	if err != nil {
		return nil, err
	}
	return coder.Encode(msg)
}

func Decode(tpy int32, data []byte, resp proto.Message) error {
	if data == nil {
		return nil
	}
	coder, err := GetCodec(tpy)
	if err != nil {
		return err
	}
	return coder.Decode(data, resp)
}

func EncodeToAny(msg any) (*anypb.Any, error) {
	data, ok := msg.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("msg is not proto.Message")
	}
	anyMsg, err := anypb.New(data)
	if err != nil {
		return nil, err
	}
	return anyMsg, nil
}

func DecodeFromAny(anyMsg *anypb.Any) (proto.Message, error) {
	if anyMsg == nil {
		return nil, nil
	}
	return anyMsg.UnmarshalNew()
}

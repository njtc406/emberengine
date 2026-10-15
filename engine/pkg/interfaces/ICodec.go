// Package interfaces
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/19 0019 18:50
// 最后更新:  yr  2025/7/19 0019 18:50
package interfaces

import (
	"google.golang.org/protobuf/proto"
)

// IEncoder 编码器接口
type IEncoder interface {
	Encode(msg interface{}) ([]byte, error)
	Type() int32
}

// IDecoder 解码器接口
type IDecoder interface {
	Decode(data []byte, resp proto.Message) error
	Type() int32
}

// ICodec 编解码器组合接口
type ICodec interface {
	IEncoder
	IDecoder
}

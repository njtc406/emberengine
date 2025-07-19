// Package interfaces
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/19 0019 18:50
// 最后更新:  yr  2025/7/19 0019 18:50
package interfaces

import "github.com/njtc406/emberengine/engine/pkg/utils/pool"

// IEncoder 编码器接口
type IEncoder interface {
	Encode(msg interface{}) ([]byte, string, error)
	Type() int32
}

// IDecoder 解码器接口
type IDecoder interface {
	Decode(typeName string, data []byte) (interface{}, error)
	Type() int32
}

// ICodec 编解码器组合接口
type ICodec interface {
	IEncoder
	IDecoder
	Stats() []*pool.Stats
}

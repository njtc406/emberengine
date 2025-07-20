// Package codec
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/19 0019 19:29
// 最后更新:  yr  2025/7/19 0019 19:29
package codec

import (
	"fmt"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
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

func Encode(typ int32, msg interface{}) ([]byte, string, error) {
	coder, err := GetCodec(typ)
	if err != nil {
		return nil, "", err
	}
	return coder.Encode(msg)
}

func Decode(tpy int32, typeName string, data []byte) (interface{}, error) {
	if data == nil || len(data) == 0 {
		return nil, nil
	}
	coder, err := GetCodec(tpy)
	if err != nil {
		return nil, err
	}
	return coder.Decode(typeName, data)
}

func Stats() []*pool.Stats {
	var stats []*pool.Stats
	for _, coder := range codecs {
		stats = append(stats, coder.Stats()...)
	}
	return stats
}

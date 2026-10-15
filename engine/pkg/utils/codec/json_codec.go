// Package codec
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/19 0019 19:06
// 最后更新:  yr  2025/7/19 0019 19:06
package codec

import (
	"fmt"

	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func init() {
	RegisterCodec(NewJsonCodec())
}

type jsonCodec struct {
	opts          protojson.MarshalOptions
	unmarshalOpts protojson.UnmarshalOptions
}

func NewJsonCodec() inf.ICodec {
	return &jsonCodec{
		opts: protojson.MarshalOptions{
			UseProtoNames:   true, // 字段名为 proto 字段名（非驼峰）
			EmitUnpopulated: true, // 零值字段也会序列化，便于调试
		},
		unmarshalOpts: protojson.UnmarshalOptions{
			DiscardUnknown: true,
		},
	}
}

func (j *jsonCodec) Type() int32 {
	return def.Json
}

func (j *jsonCodec) Encode(msg interface{}) ([]byte, error) {
	pb, ok := msg.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("jsonCodec: msg must be proto.Message")
	}
	data, err := j.opts.Marshal(pb)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func (j *jsonCodec) Decode(data []byte, resp proto.Message) error {
	return protojson.Unmarshal(data, resp)
}

func (j *jsonCodec) Stats() []*pool.Stats {
	return nil
}

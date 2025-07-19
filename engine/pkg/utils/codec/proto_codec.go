// Package codec
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/19 0019 18:49
// 最后更新:  yr  2025/7/19 0019 18:49
package codec

import (
	"fmt"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
	"google.golang.org/protobuf/proto"
)

func init() {
	RegisterCodec(NewProtoCodec())
}

type protoCodec struct {
	opts       proto.MarshalOptions
	bufferPool *BytePoolManager
}

func NewProtoCodec() inf.ICodec {
	return &protoCodec{
		opts: proto.MarshalOptions{
			AllowPartial:  true,
			Deterministic: true,
		},
		bufferPool: NewBytePoolManager([]int{32 * 1023, 64 * 1023, 128 * 1024, 512 * 1024, 1024 * 1024, 2048 * 1024}),
	}
}

func (c *protoCodec) Type() int32 {
	return def.ProtoBuf
}

func (c *protoCodec) Encode(msg interface{}) ([]byte, string, error) {
	pb, ok := msg.(proto.Message)
	if !ok {
		return nil, "", fmt.Errorf("protoCodec: msg must be proto.Message")
	}
	size := proto.Size(pb)
	bufPtr := c.bufferPool.GetPool(size)
	buf := bufPtr.Get()
	defer bufPtr.Put(buf)

	out, err := c.opts.MarshalAppend(buf.Get(), pb)
	if err != nil {
		return nil, "", err
	}
	typeName := getProtoTypeName(msg.(proto.Message))
	// 注意：一定要 copy 否则原始 buffer 会被覆盖
	copied := make([]byte, len(out))
	copy(copied, out)
	return copied, typeName, nil
}

func (c *protoCodec) Decode(typeName string, data []byte) (interface{}, error) {
	t, err := getProtoType(typeName)
	if err != nil {
		return nil, err
	}
	msg := t.New().Interface()
	return msg, proto.Unmarshal(data, msg)
}

func (c *protoCodec) Stats() []*pool.Stats {
	return c.bufferPool.Stats()
}

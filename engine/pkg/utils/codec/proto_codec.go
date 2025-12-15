// Package codec
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/19 0019 18:49
// 最后更新:  yr  2025/7/19 0019 18:49
package codec

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
	"google.golang.org/protobuf/proto"
)

var (
	protoDeterministicOnce sync.Once
	protoDeterministic     bool
)

func getProtoDeterministic() bool {
	protoDeterministicOnce.Do(func() {
		// 性能优先：默认不做 deterministic 编码。
		// deterministic 会对 map 等字段做稳定排序，CPU 开销明显；仅在确实需要“字节级稳定输出”时开启。
		protoDeterministic = false
		if v := strings.TrimSpace(os.Getenv("PROTO_DETERMINISTIC")); v != "" {
			switch strings.ToLower(v) {
			case "1", "true", "yes", "on":
				protoDeterministic = true
			}
		}
	})
	return protoDeterministic
}

func init() {
	RegisterCodec(NewProtoCodec())
}

type protoCodec struct {
	opts       proto.MarshalOptions
	bufferPool *BytePoolManager
}

func NewProtoCodec() inf.ICodec {
	deterministic := getProtoDeterministic()
	return &protoCodec{
		opts: proto.MarshalOptions{
			AllowPartial:  true,
			Deterministic: deterministic,
		},
		bufferPool: bytePoolMgr,
	}
}

func (c *protoCodec) Type() int32 {
	return def.ProtoBuf
}

func (c *protoCodec) Encode(msg interface{}) ([]byte, error) {
	pb, ok := msg.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("protoCodec: msg must be proto.Message")
	}
	// 直接按预计大小分配一次，避免使用池后再做二次 copy。
	// 旧实现为了防止池复用覆盖数据，必须 copy；这里不走池，因此无需 copy。
	size := proto.Size(pb)
	out := make([]byte, 0, size)
	out, err := c.opts.MarshalAppend(out, pb)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *protoCodec) Decode(data []byte, resp proto.Message) error {
	err := proto.Unmarshal(data, resp)
	if err != nil {
		return err
	}
	return nil
}

func (c *protoCodec) Stats() []*pool.Stats {
	return c.bufferPool.Stats()
}

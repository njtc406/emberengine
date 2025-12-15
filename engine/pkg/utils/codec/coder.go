// Package codec
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/19 0019 19:29
// 最后更新:  yr  2025/7/19 0019 19:29
package codec

import (
	"fmt"
	"sync"

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

// ========== TypeUrl 缓存优化 ==========
// protobuf 的 anypb.New() 每次都会重新生成 TypeUrl 字符串，
// 但同一个消息类型的 TypeUrl 是固定的，因此可以缓存。

var (
	typeUrlCache   = make(map[string]string) // fullName -> typeUrl
	typeUrlCacheMu sync.RWMutex
)

const typeUrlPrefix = "type.googleapis.com/"

// getTypeUrl 获取消息类型的 TypeUrl（带缓存）
func getTypeUrl(m proto.Message) string {
	fullName := string(m.ProtoReflect().Descriptor().FullName())

	// 快速路径：读锁查缓存
	typeUrlCacheMu.RLock()
	if url, ok := typeUrlCache[fullName]; ok {
		typeUrlCacheMu.RUnlock()
		return url
	}
	typeUrlCacheMu.RUnlock()

	// 慢速路径：写锁添加缓存
	typeUrlCacheMu.Lock()
	defer typeUrlCacheMu.Unlock()

	// 双检锁
	if url, ok := typeUrlCache[fullName]; ok {
		return url
	}

	url := typeUrlPrefix + fullName
	typeUrlCache[fullName] = url
	return url
}

// anyPool 复用 anypb.Any 对象（仅用于临时编码，不可长期持有）
var anyPool = sync.Pool{
	New: func() interface{} {
		return &anypb.Any{}
	},
}

// EncodeToAny 优化版本：使用 TypeUrl 缓存 + MarshalAppend
func EncodeToAny(msg any) (*anypb.Any, error) {
	data, ok := msg.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("msg is not proto.Message")
	}

	// 获取缓存的 TypeUrl
	typeUrl := getTypeUrl(data)

	// 预分配精确大小的 buffer
	size := proto.Size(data)
	buf := make([]byte, 0, size)

	// 使用 MarshalAppend 避免二次分配
	var err error
	buf, err = proto.MarshalOptions{}.MarshalAppend(buf, data)
	if err != nil {
		return nil, err
	}

	// 直接构造 Any，避免 anypb.New 的重复序列化
	return &anypb.Any{
		TypeUrl: typeUrl,
		Value:   buf,
	}, nil
}

func DecodeFromAny(anyMsg *anypb.Any) (proto.Message, error) {
	if anyMsg == nil {
		return nil, nil
	}
	return anyMsg.UnmarshalNew()
}

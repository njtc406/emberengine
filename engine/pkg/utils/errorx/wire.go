package errorx

import (
	"fmt"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"google.golang.org/protobuf/proto"
)

const maxChainDepth = 32 // 防止恶意/循环链路导致无限递归

// MarshalToBytes 将 error 序列化为 protobuf 字节，适合放入 proto bytes 字段。
//   - *Error → ErrorDetail protobuf 编码（保留 code/fields/chain/caller）
//   - 其他 error → 仅 msg 的 ErrorDetail
//   - nil → nil
func MarshalToBytes(err error) []byte {
	if err == nil {
		return nil
	}
	var detail *actor.ErrorDetail
	if ex, ok := err.(*Error); ok {
		detail = errorToProto(ex, 0)
	} else {
		detail = &actor.ErrorDetail{Msg: err.Error()}
	}
	b, marshalErr := proto.Marshal(detail)
	if marshalErr != nil {
		// proto 编码失败（几乎不可能），fallback 为纯消息
		fallback := &actor.ErrorDetail{Msg: err.Error()}
		b, _ = proto.Marshal(fallback)
	}
	return b
}

// UnmarshalFromBytes 从 protobuf 字节还原 error。
//   - 有效 protobuf → 还原为 *Error（完整 code/fields/chain）
//   - nil/空 → nil
//   - 无效数据 → *Error{msg: 原始字节的字符串表示}
func UnmarshalFromBytes(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	var detail actor.ErrorDetail
	if err := proto.Unmarshal(data, &detail); err != nil {
		return &Error{msg: fmt.Sprintf("(unmarshal error failed: %v) raw=%x", err, data)}
	}
	return protoToError(&detail, 0)
}

func errorToProto(e *Error, depth int) *actor.ErrorDetail {
	if e == nil || depth > maxChainDepth {
		return nil
	}
	d := &actor.ErrorDetail{
		Code: int32(e.code),
		Msg:  e.msg,
		File: e.file,
		Line: int32(e.line),
	}
	if len(e.fields) > 0 {
		d.Fields = make([]*actor.ErrorField, len(e.fields))
		for i, f := range e.fields {
			d.Fields[i] = &actor.ErrorField{
				Key: f.Key,
				Val: fmt.Sprint(f.Val),
			}
		}
	}
	if e.cause != nil {
		if ex, ok := e.cause.(*Error); ok {
			d.Cause = errorToProto(ex, depth+1)
		} else {
			// 非 *Error 的 cause 退化为仅 msg 的节点
			d.Cause = &actor.ErrorDetail{Msg: e.cause.Error()}
		}
	}
	return d
}

func protoToError(d *actor.ErrorDetail, depth int) *Error {
	if d == nil || depth > maxChainDepth {
		return nil
	}
	e := &Error{
		code: int(d.Code),
		msg:  d.Msg,
		file: d.File,
		line: int(d.Line),
	}
	if len(d.Fields) > 0 {
		e.fields = make([]Field, len(d.Fields))
		for i, f := range d.Fields {
			e.fields[i] = Field{Key: f.Key, Val: f.Val}
		}
	}
	if d.Cause != nil {
		e.cause = protoToError(d.Cause, depth+1)
	}
	return e
}

// Package interfaces
// @Title  rpc数据信封接口
// @Description  desc
// @Author  yr  2024/11/14
// @Update  yr  2024/11/14
package interfaces

import (
	"context"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	"google.golang.org/protobuf/types/known/anypb"
)

type IEnvelope interface {
	IDataDef

	// Set

	SetMeta(meta IEnvelopeMeta)
	SetData(data IEnvelopeData)
	SetPriority(priority def.Priority)
	SetDispatchKey(key string)

	// Get

	GetMeta() IEnvelopeMeta
	GetData() IEnvelopeData
	GetPriority() def.Priority
	GetDispatchKey() string

	// Option

	ToProtoMsg(ctx context.Context) (*actor.Message, error)

	// Release 释放信封资源
	Release()
}

type IEnvelopeMeta interface {
	IReset
	// Set

	SetSenderPid(sender *actor.PID)
	SetReceiverPid(receiver *actor.PID)
	SetDispatcher(client IRpcDispatcher)
	SetReqId(reqId uint64)
	SetDeadline(deadline int64)
	SetCallbacks(callbacks dto.CompletionFuncs, cbParams []interface{})

	// Get

	GetSenderPid() *actor.PID
	GetReceiverPid() *actor.PID
	GetDispatcher() IRpcDispatcher
	GetReqId() uint64
	GetDeadline() int64
	GetCallback() (dto.CompletionFuncs, []interface{})
}

type IEnvelopeData interface {
	IReset
	// Set

	SetMethod(method string)
	SetReply()
	SetRequest(req interface{})
	SetResponse(res interface{})
	SetError(err error)
	SetNeedResponse(need bool)

	// Get

	GetMethod() string
	GetRequest() interface{}
	GetResponse() interface{}
	GetError() error
	GetRequestBuff() (*anypb.Any, error)

	// Check

	IsReply() bool      // 是否是回复
	NeedResponse() bool // 是否需要回复
}

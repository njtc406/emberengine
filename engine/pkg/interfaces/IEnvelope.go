// Package interfaces
// @Title  信封接口
// @Description  desc
// @Author  yr  2024/11/14
// @Update  yr  2024/11/14
package interfaces

import (
	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	"time"
)

type IEnvelope interface {
	IDataDef
	IEvent
	IContext

	// Set

	SetMeta(meta IEnvelopeMeta)
	SetData(data IEnvelopeData)

	// Get

	GetMeta() IEnvelopeMeta
	GetData() IEnvelopeData

	// Option

	SetDone()
	RunCompletions()
	Wait()
	ToProtoMsg() *actor.Message
	Clone() IEnvelope
}

type IEnvelopeMeta interface {
	IReset
	// Set

	SetSenderPid(sender *actor.PID)
	SetReceiverPid(receiver *actor.PID)
	SetDispatcher(client IRpcDispatcher)
	SetTimeout(timeout time.Duration)
	SetReqId(reqId uint64)
	SetCallback(cbs []dto.CompletionFunc)
	SetTimerId(id uint64)
	SetCallbackParams(params []interface{})
	SetDone()

	// Get

	GetSenderPid() *actor.PID
	GetReceiverPid() *actor.PID
	GetDispatcher() IRpcDispatcher
	GetTimeout() time.Duration
	GetReqId() uint64
	GetTimerId() uint64
	GetCallBacks() []dto.CompletionFunc
	GetCallbackParams() []interface{}
	GetDone() <-chan struct{}

	// check

	NeedCallback() bool // 是否需要回调

	// Option

	Clone() IEnvelopeMeta
}

type IEnvelopeData interface {
	IReset
	// Set

	SetMethod(method string)
	SetReply()
	SetRequest(req interface{})
	SetResponse(res interface{})
	SetError(err error)
	SetErrStr(err string)
	SetNeedResponse(need bool)
	SetRequestBuff(reqBuff []byte)

	// Get

	GetMethod() string
	GetRequest() interface{}
	GetResponse() interface{}
	GetError() error
	GetErrStr() string
	GetRequestBuff(int32) ([]byte, string, error)

	// Check

	IsReply() bool      // 是否是回复
	NeedResponse() bool // 是否需要回复
}

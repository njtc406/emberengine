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

	// Set

	SetHeaders(header dto.Header)
	SetHeader(key string, value string)
	SetSenderPid(sender *actor.PID)
	SetReceiverPid(receiver *actor.PID)
	SetDispatcher(client IRpcDispatcher)
	SetMethod(method string)
	SetReqId(reqId uint64)
	SetReply()
	SetTimeout(timeout time.Duration)
	SetRequest(req interface{})
	SetResponse(res interface{})
	SetError(err error)
	SetErrStr(err string)
	SetNeedResponse(need bool)
	SetCallback(cbs []dto.CompletionFunc)
	SetTimerId(id uint64)
	SetCallbackParams(params []interface{})

	// Get

	IEvent
	GetHeader(key string) string
	GetHeaders() dto.Header
	GetSenderPid() *actor.PID
	GetReceiverPid() *actor.PID
	GetDispatcher() IRpcDispatcher
	GetMethod() string
	GetReqId() uint64
	GetRequest() interface{}
	GetResponse() interface{}
	GetError() error
	GetErrStr() string
	GetTimeout() time.Duration
	GetTimerId() uint64
	GetCallbackParams() []interface{}

	// Check

	NeedCallback() bool // 是否需要回调
	IsReply() bool      // 是否是回复
	NeedResponse() bool // 是否需要回复

	// Option

	Done()
	RunCompletions()
	Wait()
	ToProtoMsg() *actor.Message
}

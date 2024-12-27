// Package inf
// @Title  信封接口
// @Description  desc
// @Author  yr  2024/11/14
// @Update  yr  2024/11/14
package inf

import (
	"github.com/njtc406/emberengine/engine/actor"
	"github.com/njtc406/emberengine/engine/dto"
	"time"
)

type IEnvelope interface {
	IDataDef
	// Set

	SetHeaders(header dto.Header)
	SetHeader(key string, value string)
	SetSenderPid(sender *actor.PID)
	SetReceiverPid(receiver *actor.PID)
	SetSender(client IRpcSender)
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

	// Get

	GetHeader(key string) string
	GetHeaders() dto.Header
	GetSenderPid() *actor.PID
	GetReceiverPid() *actor.PID
	GetSender() IRpcSender
	GetMethod() string
	GetReqId() uint64
	GetRequest() interface{}
	GetResponse() interface{}
	GetError() error
	GetErrStr() string
	GetTimeout() time.Duration

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

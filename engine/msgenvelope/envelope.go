// Package msgenvelope
// @Title  数据信封
// @Description  用于不同service之间的数据传递
// @Author  yr  2024/9/2 下午3:40
// @Update  yr  2024/9/2 下午3:40
package msgenvelope

import (
	"errors"
	"github.com/njtc406/emberengine/engine/actor"
	"github.com/njtc406/emberengine/engine/dto"
	"github.com/njtc406/emberengine/engine/inf"
	"github.com/njtc406/emberengine/engine/utils/log"
	"github.com/njtc406/emberengine/engine/utils/pool"
	"github.com/njtc406/emberengine/engine/utils/serializer"
	"sync"
	"time"
)

type MsgEnvelope struct {
	dto.DataRef
	// 可能会在多线程环境下面被操作,所以需要锁!
	locker *sync.RWMutex

	senderPid   *actor.PID           // 发送者
	receiverPid *actor.PID           // 接收者
	sender      inf.IRpcSender       // 发送者客户端(用于回调)
	method      string               // 调用方法
	reqID       uint64               // 请求ID(防止重复,目前还未做防重复逻辑)
	reply       bool                 // 是否是回复
	header      dto.Header           // 消息头
	timeout     time.Duration        // 请求超时时间
	request     interface{}          // 请求参数
	response    interface{}          // 回复数据
	needResp    bool                 // 是否需要回复
	err         error                // 错误
	callbacks   []dto.CompletionFunc // 完成回调
	done        chan struct{}        // 完成信号
}

func (e *MsgEnvelope) Reset() {
	if e.locker != nil {
		e.locker.Lock()
		defer e.locker.Unlock()
	} else {
		e.locker = &sync.RWMutex{}
	}

	e.senderPid = nil
	e.receiverPid = nil
	e.sender = nil
	e.method = ""
	e.reqID = 0
	e.reply = false
	e.header = nil
	e.timeout = 0
	e.request = nil
	e.response = nil
	e.needResp = false
	if e.done == nil {
		e.done = make(chan struct{}, 1)
	}
	if len(e.done) > 0 {
		<-e.done
	}
	e.err = nil
	e.callbacks = e.callbacks[:0]
}

func (e *MsgEnvelope) SetHeaders(header dto.Header) {
	e.locker.Lock()
	defer e.locker.Unlock()
	for k, v := range header {
		e.SetHeader(k, v)
	}
}

func (e *MsgEnvelope) SetHeader(key string, value string) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.header.Set(key, value)
}

func (e *MsgEnvelope) SetSenderPid(sender *actor.PID) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.senderPid = sender
}

func (e *MsgEnvelope) SetReceiverPid(receiver *actor.PID) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.receiverPid = receiver
}

func (e *MsgEnvelope) SetSender(client inf.IRpcSender) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.sender = client
}

func (e *MsgEnvelope) SetMethod(method string) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.method = method
}

func (e *MsgEnvelope) SetReqId(reqId uint64) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.reqID = reqId
}

func (e *MsgEnvelope) SetReply() {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.reply = true
}

func (e *MsgEnvelope) SetTimeout(timeout time.Duration) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.timeout = timeout
}

func (e *MsgEnvelope) SetRequest(req interface{}) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.request = req
}

func (e *MsgEnvelope) SetResponse(res interface{}) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.response = res
}

func (e *MsgEnvelope) SetError(err error) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.err = err
}

func (e *MsgEnvelope) SetErrStr(err string) {
	if err == "" {
		return
	}
	e.locker.Lock()
	defer e.locker.Unlock()
	e.err = errors.New(err)
}

func (e *MsgEnvelope) SetNeedResponse(need bool) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.needResp = need
}

func (e *MsgEnvelope) SetCallback(cbs []dto.CompletionFunc) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.callbacks = append(e.callbacks, cbs...)
}

func (e *MsgEnvelope) GetHeader(key string) string {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.header.Get(key)
}

func (e *MsgEnvelope) GetHeaders() dto.Header {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.header
}

func (e *MsgEnvelope) GetSenderPid() *actor.PID {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.senderPid
}

func (e *MsgEnvelope) GetReceiverPid() *actor.PID {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.receiverPid
}

func (e *MsgEnvelope) GetSender() inf.IRpcSender {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.sender
}

func (e *MsgEnvelope) GetMethod() string {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.method
}

func (e *MsgEnvelope) GetReqId() uint64 {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.reqID
}

func (e *MsgEnvelope) GetRequest() interface{} {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.request
}

func (e *MsgEnvelope) GetResponse() interface{} {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.response
}

func (e *MsgEnvelope) GetError() error {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.err
}

func (e *MsgEnvelope) GetErrStr() string {
	e.locker.RLock()
	defer e.locker.RUnlock()
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *MsgEnvelope) GetTimeout() time.Duration {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.timeout
}

func (e *MsgEnvelope) NeedCallback() bool {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return len(e.callbacks) > 0
}

func (e *MsgEnvelope) IsReply() bool {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.reply
}

func (e *MsgEnvelope) NeedResponse() bool {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.needResp
}

func (e *MsgEnvelope) Done() {
	if e.done != nil {
		e.done <- struct{}{}
	} else {
		log.SysLogger.Warn("=================envelope done is nil===================")
	}
}

func (e *MsgEnvelope) RunCompletions() {
	for _, cb := range e.callbacks {
		cb(e.response, e.err)
	}
}

func (e *MsgEnvelope) Wait() {
	<-e.done
}

func (e *MsgEnvelope) ToProtoMsg() *actor.Message {
	e.locker.RLock()
	defer e.locker.RUnlock()
	msg := &actor.Message{
		TypeId:        0, // 默认使用protobuf(后面有其他需求再修改这里)
		TypeName:      "",
		SenderPid:     e.senderPid,
		ReceiverPid:   e.receiverPid,
		Method:        e.method,
		Request:       nil,
		Response:      nil,
		Err:           e.GetErrStr(),
		MessageHeader: e.header,
		Reply:         e.reply,
		ReqId:         e.reqID,
		NeedResp:      e.needResp,
	}

	var byteData []byte
	var typeName string
	var err error

	if e.request != nil {
		byteData, typeName, err = serializer.Serialize(e.request, msg.TypeId)
		if err != nil {
			log.SysLogger.Errorf("serialize message[%+v] is error: %s", e, err)
			return nil
		}
		msg.Request = byteData
	} else if e.response != nil {
		byteData, typeName, err = serializer.Serialize(e.response, msg.TypeId)
		if err != nil {
			log.SysLogger.Errorf("serialize message[%+v] is error: %s", e, err)
			return nil
		}
		msg.Response = byteData
	}

	msg.TypeName = typeName

	return msg
}

//======================================================

var msgEnvelopePool = pool.NewPoolEx(make(chan pool.IPoolData, 10240), func() pool.IPoolData {
	return &MsgEnvelope{}
})

// 测试资源释放
var count int

func NewMsgEnvelope() *MsgEnvelope {
	count++
	//log.SysLogger.Infof(">>>>>>>>>>>>>>>>>>>>>>>>>>>>>msgEnvelopePool.Get() count: %d", count)
	return msgEnvelopePool.Get().(*MsgEnvelope)
}

func ReleaseMsgEnvelope(envelope inf.IEnvelope) {
	if envelope != nil {
		count--
		//log.SysLogger.Infof("<<<<<<<<<<<<<<<<<<<<<<<<<<<msgEnvelopePool.Put() count: %d", count)
		msgEnvelopePool.Put(envelope.(*MsgEnvelope))
	}
}
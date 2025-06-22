// Package msgenvelope
// @Title  数据信封
// @Description  用于不同service之间的数据传递
// @Author  yr  2024/9/2 下午3:40
// @Update  yr  2024/9/2 下午3:40
package msgenvelope

import (
	"errors"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/utils/emberctx"
	"golang.org/x/net/context"
	"strconv"
	"sync"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
	"github.com/njtc406/emberengine/engine/pkg/utils/serializer"
)

var msgEnvelopePool = pool.NewPoolEx(make(chan pool.IPoolData, 10240), func() pool.IPoolData {
	return &MsgEnvelope{}
})
var metaPool = pool.NewPoolEx(make(chan pool.IPoolData, 10240), func() pool.IPoolData {
	return &Meta{}
})

func NewMeta() inf.IEnvelopeMeta {
	return metaPool.Get().(inf.IEnvelopeMeta)
}

func NewData() inf.IEnvelopeData {
	return &Data{} // data不使用缓存池, 因为多个共用相同的data时,前面的释放会导致后面的都取不到了
}

type Meta struct {
	dto.DataRef
	locker sync.RWMutex

	senderPid   *actor.PID         // 发送者
	receiverPid *actor.PID         // 接收者
	sender      inf.IRpcDispatcher // 发送者客户端(用于回调)
	// 缓存信息
	timeout        time.Duration        // 请求超时时间
	done           chan struct{}        // 完成信号
	reqID          uint64               // 请求ID(主要用于monitor区分不同的call)
	timerId        uint64               // 定时器ID
	callbacks      []dto.CompletionFunc // 完成回调
	callbackParams []interface{}        // 回调透传参数
}

func (e *Meta) Reset() {
	e.senderPid = nil
	e.receiverPid = nil
	e.sender = nil
	e.timeout = 0
	e.reqID = 0
	if e.done == nil {
		e.done = make(chan struct{}, 1)
	}
	if len(e.done) > 0 {
		<-e.done
	}
	e.callbacks = e.callbacks[:0]
}

func (e *Meta) SetSenderPid(senderPid *actor.PID) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.senderPid = senderPid
}

func (e *Meta) SetReceiverPid(receiverPid *actor.PID) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.receiverPid = receiverPid
}

func (e *Meta) SetDispatcher(client inf.IRpcDispatcher) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.sender = client
}

func (e *Meta) SetTimeout(timeout time.Duration) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.timeout = timeout
}

func (e *Meta) SetReqId(reqId uint64) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.reqID = reqId
}

func (e *Meta) SetCallback(cbs []dto.CompletionFunc) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.callbacks = cbs
}

func (e *Meta) SetTimerId(id uint64) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.timerId = id
}

func (e *Meta) SetCallbackParams(params []interface{}) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.callbackParams = params
}

func (e *Meta) SetDone() {
	e.done <- struct{}{}
}

func (e *Meta) GetSenderPid() *actor.PID {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.senderPid
}

func (e *Meta) GetReceiverPid() *actor.PID {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.receiverPid
}

func (e *Meta) GetDispatcher() inf.IRpcDispatcher {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.sender
}

func (e *Meta) GetTimeout() time.Duration {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.timeout
}

func (e *Meta) GetReqId() uint64 {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.reqID
}

func (e *Meta) GetTimerId() uint64 {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.timerId
}

func (e *Meta) GetCallBacks() []dto.CompletionFunc {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.callbacks[:] // 返回一个快照
}

func (e *Meta) GetCallbackParams() []interface{} {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.callbackParams[:]
}

func (e *Meta) GetDone() <-chan struct{} {
	return e.done
}

func (e *Meta) NeedCallback() bool {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return len(e.callbacks) > 0
}

type Data struct {
	locker sync.RWMutex
	// 数据包
	method      string      // 调用方法
	reply       bool        // 是否是回复
	request     interface{} // 请求参数
	response    interface{} // 回复数据
	needResp    bool        // 是否需要回复
	err         error       // 错误
	requestBuff []byte      // 编码好的数据
	typeName    string      // 类型名
}

func (e *Data) Reset() {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.method = ""
	e.reply = false
	e.request = nil
	e.response = nil
	e.needResp = false
	e.err = nil
	e.requestBuff = e.requestBuff[:0]
	e.typeName = ""
}

func (e *Data) SetMethod(method string) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.method = method
}

func (e *Data) SetReply() {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.reply = true
}

func (e *Data) SetRequest(req interface{}) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.request = req
}

func (e *Data) SetResponse(res interface{}) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.response = res
}

func (e *Data) SetError(err error) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.err = err
}

func (e *Data) SetErrStr(err string) {
	e.locker.Lock()
	defer e.locker.Unlock()
	if err == "" {
		e.err = nil
		return
	}
	e.err = errors.New(err)
}

func (e *Data) SetNeedResponse(need bool) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.needResp = need
}

func (e *Data) SetRequestBuff(reqBuff []byte) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.requestBuff = reqBuff
}

func (e *Data) GetMethod() string {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.method
}

func (e *Data) GetRequest() interface{} {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.request
}

func (e *Data) GetResponse() interface{} {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.response
}

func (e *Data) GetError() error {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.err
}

func (e *Data) GetErrStr() string {
	e.locker.RLock()
	defer e.locker.RUnlock()
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *Data) GetRequestBuff(tpId int32) ([]byte, string, error) {
	e.locker.Lock()
	defer e.locker.Unlock()

	if e.request == nil {
		return nil, "", nil
	}

	if len(e.requestBuff) > 0 {
		return e.requestBuff, e.typeName, e.err
	}

	e.requestBuff, e.typeName, e.err = serializer.Serialize(e.request, tpId)

	return e.requestBuff, e.typeName, e.err
}

func (e *Data) IsReply() bool {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.reply
}

func (e *Data) NeedResponse() bool {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.needResp
}

type MsgEnvelope struct {
	dto.DataRef
	// 可能会在多线程环境下面被操作,所以需要锁!
	locker *sync.RWMutex
	ctx    context.Context

	meta *Meta
	data *Data
}

func (e *MsgEnvelope) Reset() {
	if e.locker != nil {
		e.locker.Lock()
		defer e.locker.Unlock()
	} else {
		e.locker = &sync.RWMutex{}
	}

	e.ctx = nil
	if e.meta != nil {
		e.meta.Reset()
	}
}

//-------------------------------------set-----------------------------------------

func (e *MsgEnvelope) WithContext(ctx context.Context) inf.IEnvelope {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.ctx = ctx
	return e
}

func (e *MsgEnvelope) SetHeaders(headers dto.Header) {
	if headers == nil {
		return
	}
	e.locker.Lock()
	defer e.locker.Unlock()
	if e.ctx == nil {
		e.ctx = context.Background()
	}

	e.ctx = emberctx.AddHeaders(e.ctx, headers)
}

func (e *MsgEnvelope) SetHeader(key string, value string) {
	e.locker.Lock()
	defer e.locker.Unlock()
	if e.ctx == nil {
		e.ctx = context.Background()
	}
	e.ctx = emberctx.AddHeader(e.ctx, key, value)
}

func (e *MsgEnvelope) SetMeta(meta inf.IEnvelopeMeta) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.meta = meta.(*Meta)
}

func (e *MsgEnvelope) SetData(data inf.IEnvelopeData) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.data = data.(*Data)
}

//--------------------------------get------------------------------------

func (e *MsgEnvelope) GetType() int32 {
	tp := e.GetHeader(def.DefaultTypeKey)
	if tp == "" {
		return event.RpcMsg
	} else {
		tpInt, err := strconv.Atoi(tp)
		if err != nil {
			return event.RpcMsg
		} else {
			return int32(tpInt)
		}
	}
}

func (e *MsgEnvelope) GetKey() string {
	return e.GetHeader(def.DefaultDispatcherKey)
}

func (e *MsgEnvelope) GetPriority() int32 {
	priority := e.GetHeader(def.DefaultPriorityKey)
	if priority == "" {
		return def.PriorityUser
	} else {
		priorityInt, err := strconv.Atoi(priority)
		if err != nil {
			return def.PriorityUser
		} else {
			return int32(priorityInt)
		}
	}
}

func (e *MsgEnvelope) GetHeader(key string) string {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return emberctx.GetHeaderValue(e.ctx, key)
}

func (e *MsgEnvelope) GetHeaders() dto.Header {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return emberctx.GetHeader(e.ctx)
}

func (e *MsgEnvelope) GetContext() context.Context {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.ctx
}

func (e *MsgEnvelope) GetMeta() inf.IEnvelopeMeta {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.meta
}

func (e *MsgEnvelope) GetData() inf.IEnvelopeData {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.data
}

//-----------------------------Option-----------------------------------

func (e *MsgEnvelope) Done() {
	e.meta.SetDone()
}

func (e *MsgEnvelope) RunCompletions() {
	for _, cb := range e.meta.GetCallBacks() {
		cb(e.meta.GetCallbackParams(), e.data.GetResponse(), e.data.GetError())
	}
}

func (e *MsgEnvelope) Wait() {
	<-e.meta.GetDone()
}

func (e *MsgEnvelope) ToProtoMsg() *actor.Message {
	e.locker.RLock()
	defer e.locker.RUnlock()
	// TODO message使用缓存池来获取
	msg := &actor.Message{
		TypeId:        0, // 默认使用protobuf(后面有其他需求再修改这里)
		TypeName:      "",
		SenderPid:     e.meta.GetSenderPid(),
		ReceiverPid:   e.meta.GetReceiverPid(),
		Method:        e.data.GetMethod(),
		Request:       nil,
		Response:      nil,
		Err:           e.data.GetErrStr(),
		MessageHeader: emberctx.GetHeader(e.ctx),
		Reply:         e.data.IsReply(),
		ReqId:         e.meta.GetReqId(),
		NeedResp:      e.data.NeedResponse(),
	}

	var byteData []byte
	var typeName string
	var err error

	byteData, typeName, err = e.data.GetRequestBuff(msg.TypeId)
	if err != nil {
		log.SysLogger.Errorf("serialize message[%+v] is error: %s", e, err)
		return nil
	}

	msg.Request = byteData

	if resp := e.data.GetResponse(); resp != nil {
		byteData, typeName, err = serializer.Serialize(resp, msg.TypeId)
		if err != nil {
			log.SysLogger.Errorf("serialize message[%+v] is error: %s", e, err)
			return nil
		}
		msg.Response = byteData
		// TODO 这里要不要置空返回,按理一个返回只能有一个人取
	}

	msg.TypeName = typeName

	return msg
}

func (e *MsgEnvelope) IncRef() {
	// envelope不会被并发处理,每个envelope只会被唯一一个服务处理,只有data是共享
	// 所以不需要引用计数
}

func (e *MsgEnvelope) Release() {
	if e.IsRef() {
		//count--
		//log.SysLogger.Infof("<<<<<<<<<<<<<<<<<<<<<<<<<<<msgEnvelopePool.Put() count: %d", count)
		metaPool.Put(e.meta)
		msgEnvelopePool.Put(e)
	}
}

func NewMsgEnvelope() *MsgEnvelope {
	//count++
	//log.SysLogger.Infof(">>>>>>>>>>>>>>>>>>>>>>>>>>>>>msgEnvelopePool.Get() count: %d", count)
	return msgEnvelopePool.Get().(*MsgEnvelope)
}

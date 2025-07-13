// Package msgenvelope
// @Title  数据信封
// @Description  用于不同service之间的数据传递
// @Author  yr  2024/9/2 下午3:40
// @Update  yr  2024/9/2 下午3:40
package msgenvelope

import (
	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
	"github.com/njtc406/emberengine/engine/pkg/utils/serializer"
	"github.com/njtc406/emberengine/engine/pkg/xcontext"
	"golang.org/x/net/context"
	"strconv"
	"sync"
)

var msgEnvelopePool = pool.NewPoolEx(make(chan pool.IPoolData, 10240), func() pool.IPoolData {
	return &MsgEnvelope{}
})

type MsgEnvelope struct {
	dto.DataRef
	// 可能会在多线程环境下面被操作,所以需要锁!
	locker *sync.RWMutex
	xcontext.XContext

	meta *Meta
	data *Data
}

func (e *MsgEnvelope) Reset() {
	if e.locker == nil {
		e.locker = &sync.RWMutex{}
	}

	e.XContext.Reset()
	e.meta = nil
	e.data = nil
}

//-------------------------------------set-----------------------------------------

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
	if tp != "" {
		tpInt, err := strconv.Atoi(tp)
		if err == nil {
			return int32(tpInt)
		}
	}
	return event.RpcMsg
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

func (e *MsgEnvelope) SetDone() {
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
		MessageHeader: e.GetHeaders(),
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

//var count int32

func (e *MsgEnvelope) Release() {
	e.locker.Lock()
	defer e.locker.Unlock()
	if e.IsRef() {
		//count--
		//log.SysLogger.Infof("<<<<<<<<<<<<<<<<<<<<<<<<<<<msgEnvelopePool.Put() count: %d", count)
		if e.meta != nil && e.meta.IsRef() {
			metaPool.Put(e.meta)
		}

		msgEnvelopePool.Put(e)
	}
}

func NewMsgEnvelope(ctx context.Context) *MsgEnvelope {
	//count++
	//log.SysLogger.Infof(">>>>>>>>>>>>>>>>>>>>>>>>>>>>>msgEnvelopePool.Get() count: %d", count)
	ep := msgEnvelopePool.Get().(*MsgEnvelope)
	ep.XContext = xcontext.New(ctx)
	return ep
}

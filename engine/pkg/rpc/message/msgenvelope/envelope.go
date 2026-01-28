// Package msgenvelope
// @Title  数据信封
// @Description  用于不同service之间的数据传递
// @Author  yr  2024/9/2 下午3:40
// @Update  yr  2024/9/2 下午3:40
package msgenvelope

import (
	"context"
	"sync"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/codec"
	"github.com/njtc406/emberengine/engine/pkg/utils/emberctx"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
	"google.golang.org/protobuf/types/known/anypb"
)

var msgEnvelopePool pool.IPool[*MsgEnvelope]
var msgEnvelopePoolOnce sync.Once

func getMsgEnvelopePool() pool.IPool[*MsgEnvelope] {
	msgEnvelopePoolOnce.Do(func() {
		msgEnvelopePool = pool.NewSyncPoolWrapper(
			func() *MsgEnvelope {
				return &MsgEnvelope{}
			},
			func() pool.IStatsRecorder {
				if config.IsDebug() {
					return pool.NewStatsRecorder("msgEnvelopePool")
				} else {
					return pool.NewNoStatsRecorder()
				}
			}(),
			pool.WithRef(func(t *MsgEnvelope) {
				t.Ref()
			}),
			pool.WithUnRef(func(t *MsgEnvelope) bool {
				return t.UnRef()
			}),
			pool.WithReset(func(t *MsgEnvelope) {
				t.Reset()
			}),
		)
	})
	return msgEnvelopePool
}

type MsgEnvelope struct {
	dto.DataRef
	// 可能会在多线程环境下面被操作,所以需要锁!
	locker *sync.RWMutex

	meta *Meta
	data *Data
}

func (e *MsgEnvelope) Reset() {
	if e.locker == nil {
		e.locker = &sync.RWMutex{}
	}

	e.meta = nil
	e.data = nil
}

//-------------------------------------set-----------------------------------------

func (e *MsgEnvelope) SetMeta(meta inf.IEnvelopeMeta) {
	e.locker.Lock()
	defer e.locker.Unlock()
	if meta == nil {
		e.meta = nil
	} else {
		e.meta = meta.(*Meta)
	}
}

func (e *MsgEnvelope) SetData(data inf.IEnvelopeData) {
	e.locker.Lock()
	defer e.locker.Unlock()
	if data == nil {
		e.data = nil
	} else {
		e.data = data.(*Data)
	}
}

//--------------------------------get------------------------------------

func (e *MsgEnvelope) GetMeta() inf.IEnvelopeMeta {
	e.locker.RLock()
	defer e.locker.RUnlock()
	if e.meta == nil {
		return nil
	}
	return e.meta
}

func (e *MsgEnvelope) GetData() inf.IEnvelopeData {
	e.locker.RLock()
	defer e.locker.RUnlock()
	if e.data == nil {
		return nil
	}
	return e.data
}

func (e *MsgEnvelope) GetType() int32 {
	// MsgEnvelope 始终是 RPC 消息类型
	return event.RpcMsg
}

func (e *MsgEnvelope) GetPriority() def.Priority {
	// TODO 需要修改为能设置优先级
	return def.PriorityNormal
}

func (e *MsgEnvelope) GetDispatcherKey() string {
	// RPC 消息不使用分发键
	return ""
}

//-----------------------------Option-----------------------------------

func (e *MsgEnvelope) ToProtoMsg(ctx context.Context) (*actor.Message, error) {
	e.locker.RLock()
	defer e.locker.RUnlock()

	if e.meta == nil || e.data == nil {
		return nil, def.ErrMsgSerializeFailed
	}
	if e.meta.GetReceiverPid() == nil {
		return nil, def.ErrServiceNotFound
	}

	var err error
	msg := NewMessage()
	defer func() {
		if err != nil {
			// 没有创建成功,需要释放msg
			ReleaseMessage(msg)
		}
	}()

	if senderPid := e.meta.GetSenderPid(); senderPid != nil {
		msg.SenderPid = senderPid
	}
	if receiverPid := e.meta.GetReceiverPid(); receiverPid != nil {
		msg.ReceiverPid = receiverPid
	}
	// 从 ctx 获取调度信息
	dispatcherKey, _ := emberctx.GetHeaderValue(ctx, def.DefaultDispatcherKey).(string)
	priority, _ := emberctx.GetHeaderValue(ctx, def.DefaultPriorityKey).(def.Priority)
	msg.Priority = int32(priority)
	msg.DispatcherKey = dispatcherKey
	msg.Method = e.data.GetMethod()
	msg.Request = nil
	msg.Response = nil
	msg.Err = e.data.GetErrStr()
	msg.ContextHeaders = emberctx.ToHeaders(ctx)
	msg.Reply = e.data.IsReply()
	msg.ReqId = e.meta.GetReqId()
	msg.NeedResp = e.data.NeedResponse()

	var anyData *anypb.Any

	if req := e.data.GetRequest(); req != nil {
		// 请求数据可能是多个请求共用的,所以只需要其中一个编码好就可以了
		anyData, err = e.data.GetRequestBuff()
		if err != nil {
			return nil, err
		}

		msg.Request = anyData
	}

	if resp := e.data.GetResponse(); resp != nil {
		// 回复数据是独立的,直接编码
		anyData, err = codec.EncodeToAny(resp)
		if err != nil {
			return nil, err
		}
		msg.Response = anyData
	}

	return msg, nil
}

func (e *MsgEnvelope) Release() {
	e.locker.Lock()
	defer e.locker.Unlock()
	if e.IsRef() {
		// envelope/meta 是可复用资源，需要回收到对象池
		// data 可能是外部创建并可能被多个服务共享的，不归 MsgEnvelope 释放
		if e.meta != nil && e.meta.IsRef() {
			putMeta(e.meta)
			e.meta = nil
		}

		getMsgEnvelopePool().Put(e)
	}
}

func NewMsgEnvelope() *MsgEnvelope {
	ep := getMsgEnvelopePool().Get()
	return ep
}

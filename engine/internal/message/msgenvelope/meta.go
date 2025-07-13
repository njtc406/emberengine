// Package msgenvelope
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/13 0013 22:21
// 最后更新:  yr  2025/7/13 0013 22:21
package msgenvelope

import (
	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
	"sync"
	"time"
)

var metaPool = pool.NewPoolEx(make(chan pool.IPoolData, 10240), func() pool.IPoolData {
	return &Meta{}
})

func NewMeta() inf.IEnvelopeMeta {
	return metaPool.Get().(inf.IEnvelopeMeta)
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

// Package monitor
// @Title  rpc调用监视器
// @Description  用于监控rpc的call调用,当超时发生时自动回调,防止一直阻塞
// @Author  pc  2024/11/6
// @Update  pc  2024/11/6
package monitor

import (
	"github.com/njtc406/emberengine/engine/dto"
	"github.com/njtc406/emberengine/engine/errdef"
	"github.com/njtc406/emberengine/engine/inf"
	"github.com/njtc406/emberengine/engine/msgenvelope"
	"github.com/njtc406/emberengine/engine/utils/log"
	"sync"
	"sync/atomic"
	"time"
)

var rpcMonitor *RpcMonitor

type RpcMonitor struct {
	closed  chan struct{}
	locker  sync.RWMutex
	seed    uint64
	waitMap map[uint64]inf.IEnvelope
	th      CallTimerHeap // 由于请求很频繁,所以这里使用单独的timer来处理
	ticker  *time.Ticker
}

func GetRpcMonitor() *RpcMonitor {
	if rpcMonitor == nil {
		rpcMonitor = &RpcMonitor{}
	}
	return rpcMonitor
}

func (rm *RpcMonitor) Init() inf.IMonitor {
	rm.closed = make(chan struct{})
	rm.waitMap = make(map[uint64]inf.IEnvelope)
	rm.th.Init()
	rm.ticker = time.NewTicker(time.Millisecond * 100)
	return rm
}

func (rm *RpcMonitor) Start() {
	go rm.listen()
}

func (rm *RpcMonitor) Stop() {
	close(rm.closed)
	rm.ticker.Stop()
}

func (rm *RpcMonitor) listen() {
	for {
		select {
		case <-rm.ticker.C:
			rm.tick()
		case <-rm.closed:
			return
		}
	}
}

func (rm *RpcMonitor) tick() {
	for i := 0; i < 1000; i++ { // 每个tick 最多处理1000个超时的rpc
		rm.locker.Lock()
		id := rm.th.PopTimeout()
		if id == 0 {
			rm.locker.Unlock()
			break
		}

		envelope := rm.waitMap[id]

		// 直接删除
		delete(rm.waitMap, id)

		if envelope == nil || !envelope.IsRef() {
			rm.locker.Unlock()
			log.SysLogger.Errorf("call seq is not find,seq:%d", id)
			continue
		}

		//log.SysLogger.Debugf("RPC call takes more than %d seconds,method is %s", int64(envelope.GetTimeout().Seconds()), envelope.GetMethod())
		// 调用超时,执行超时回调
		rm.callTimeout(envelope)
		rm.locker.Unlock()
		continue
	}
}

func (rm *RpcMonitor) GenSeq() uint64 {
	return atomic.AddUint64(&rm.seed, 1)
}

func (rm *RpcMonitor) Add(envelope inf.IEnvelope) {
	id := envelope.GetReqId()
	if id == 0 {
		return
	}
	rm.locker.Lock()
	defer rm.locker.Unlock()
	rm.waitMap[id] = envelope
	rm.th.AddTimer(id, envelope.GetTimeout())
}

func (rm *RpcMonitor) remove(id uint64) inf.IEnvelope {
	f, ok := rm.waitMap[id]
	if !ok {
		return nil
	}

	rm.th.Cancel(id)
	delete(rm.waitMap, id)
	return f
}

func (rm *RpcMonitor) Remove(id uint64) inf.IEnvelope {
	if id == 0 {
		return nil
	}
	rm.locker.Lock()
	f := rm.remove(id)
	rm.locker.Unlock()
	return f
}

func (rm *RpcMonitor) Get(id uint64) inf.IEnvelope {
	rm.locker.RLock()
	defer rm.locker.RUnlock()

	return rm.waitMap[id]
}

func (rm *RpcMonitor) callTimeout(envelope inf.IEnvelope) {
	if !envelope.IsRef() {
		log.SysLogger.Debug("envelope is not ref")
		return // 已经被释放,丢弃
	}

	envelope.SetResponse(nil)
	envelope.SetError(errdef.RPCCallTimeout)

	if envelope.NeedCallback() {
		// (这里的envelope会在两个地方回收,如果是本地调用,那么会在requestHandler执行完成后自动回收
		// 如果是远程调用,那么在远程client将消息发送完成后自动回收)
		if err := envelope.GetSender().PushRequest(envelope); err != nil {
			msgenvelope.ReleaseMsgEnvelope(envelope)
			log.SysLogger.Errorf("send call timeout response error:%s", err.Error())
		}
	} else {
		envelope.Done()
	}
}

func (rm *RpcMonitor) NewCancel(id uint64) dto.CancelRpc {
	return func() {
		rm.Remove(id)
	}
}

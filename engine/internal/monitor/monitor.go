// Package monitor
// @Title  rpc调用监视器
// @Description  用于监控rpc的call调用,当超时发生时自动回调,防止一直阻塞
// @Author  pc  2024/11/6
// @Update  pc  2024/11/6
package monitor

import (
	"github.com/njtc406/emberengine/engine/pkg/def"
	"sync"
	"sync/atomic"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
)

var rpcMonitor *RpcMonitor

type RpcMonitor struct {
	closed  chan struct{}
	locker  sync.RWMutex
	seed    uint64
	waitMap map[uint64]inf.IEnvelope
	sd      timingwheel.ITimerScheduler
	wg      sync.WaitGroup
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
	rm.sd = timingwheel.NewTaskScheduler(config.Conf.NodeConf.MonitorTimerSize, config.Conf.NodeConf.MonitorBucketSize)
	return rm
}

func (rm *RpcMonitor) Start() {
	rm.wg.Add(1)
	go rm.listen()
}

func (rm *RpcMonitor) Stop() {
	close(rm.closed)
	rm.sd.Stop()
	rm.wg.Wait()
}

func (rm *RpcMonitor) listen() {
	defer rm.wg.Done()
	for {
		select {
		case t := <-rm.sd.GetTimerCbChannel():
			if t == nil {
				continue
			}
			//log.SysLogger.Debugf("RPC monitor starts executing timeout callback:%s", t.GetName())
			t.Do()
		case <-rm.closed:
			return
		}
	}
}

func (rm *RpcMonitor) GenSeq() uint64 {
	return atomic.AddUint64(&rm.seed, 1)
}

func (rm *RpcMonitor) Add(envelope inf.IEnvelope) {
	rm.locker.Lock()
	defer rm.locker.Unlock()

	timerId, err := rm.sd.AfterFuncWithStorage(envelope.GetTimeout(), "rpc monitor", func(tm *timingwheel.Timer, args ...interface{}) {
		envelope := args[0].(inf.IEnvelope)
		rm.locker.Lock()
		// 直接删除
		delete(rm.waitMap, tm.GetTimerId())
		rm.locker.Unlock()

		if envelope == nil || !envelope.IsRef() {
			log.SysLogger.WithContext(envelope.GetContext()).Errorf("call seq is not find,seq:%d", tm.GetTimerId())
			return
		}

		log.SysLogger.WithContext(envelope.GetContext()).Debugf("RPC call takes more than %d seconds,method is %s", int64(envelope.GetTimeout().Seconds()), envelope.GetMethod())
		// 调用超时,执行超时回调
		rm.callTimeout(envelope)
	}, envelope)
	if err != nil {
		log.SysLogger.WithContext(envelope.GetContext()).Errorf("add monitor failed,error:%s", err)
		return
	}
	envelope.SetTimerId(timerId)
	rm.waitMap[envelope.GetReqId()] = envelope
}

func (rm *RpcMonitor) remove(seqId uint64) inf.IEnvelope {
	envelope, ok := rm.waitMap[seqId]
	if !ok {
		return nil
	}

	if !rm.sd.Cancel(envelope.GetTimerId()) {
		log.SysLogger.WithContext(envelope.GetContext()).Errorf("cancel monitor failed,seq:%d", seqId)
	}
	delete(rm.waitMap, seqId)
	return envelope
}

func (rm *RpcMonitor) Remove(seqId uint64) inf.IEnvelope {
	if seqId == 0 {
		return nil
	}
	rm.locker.Lock()
	f := rm.remove(seqId)
	rm.locker.Unlock()
	return f
}

func (rm *RpcMonitor) Get(seqId uint64) inf.IEnvelope {
	rm.locker.RLock()
	defer rm.locker.RUnlock()

	return rm.waitMap[seqId]
}

func (rm *RpcMonitor) callTimeout(envelope inf.IEnvelope) {
	if !envelope.IsRef() {
		log.SysLogger.WithContext(envelope.GetContext()).Debug("envelope is not ref")
		return // 已经被释放,丢弃
	}

	envelope.SetResponse(nil)
	envelope.SetError(def.RPCCallTimeout)

	if envelope.NeedCallback() {
		// (这里的envelope会在两个地方回收,如果是本地调用,那么会在requestHandler执行完成后自动回收
		// 如果是远程调用,那么在远程client将消息发送完成后自动回收)
		if err := envelope.GetDispatcher().PostMessage(envelope); err != nil {
			envelope.Release()
			log.SysLogger.WithContext(envelope.GetContext()).Errorf("send call timeout response error:%s", err.Error())
		}
	} else {
		envelope.Done()
	}
}

func (rm *RpcMonitor) NewCancel(seqId uint64) dto.CancelRpc {
	return func() {
		rm.Remove(seqId)
	}
}

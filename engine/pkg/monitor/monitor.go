// Package monitor
// @Title  rpc调用监视器
// @Description  用于监控rpc的call调用,当超时发生时自动回调,防止一直阻塞
// @Author  pc  2024/11/6
// @Update  pc  2024/11/6
package monitor

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/asynclib"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
)

var rpcMonitor *RpcMonitor

type RpcMonitor struct {
	closed  atomic.Bool
	ctx     context.Context
	cancel  context.CancelFunc
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
	ctx, cancel := context.WithCancel(context.Background())
	rm.ctx = ctx
	rm.cancel = cancel
	rm.waitMap = make(map[uint64]inf.IEnvelope)
	rm.sd = timingwheel.NewJobScheduler("rpc monitor", config.Conf.NodeConf.MonitorTimerSize, config.Conf.NodeConf.MonitorBucketSize,
		timingwheel.GetTimingWheel(), log.SysLogger.WithField("component", "rpc monitor"), config.IsDebug())
	return rm
}

func (rm *RpcMonitor) Start() {
	if rm.closed.Load() {
		return
	}
	if rm.sd == nil {
		log.SysLogger.Panic("rpc monitor is not initialized")
	}
	rm.wg.Add(1)
	go rm.listen()
}

func (rm *RpcMonitor) Stop() {
	if !rm.closed.CompareAndSwap(false, true) {
		return
	}
	rm.cancel()
	if rm.sd != nil {
		rm.sd.Stop()
	}
	rm.wg.Wait()
}

func (rm *RpcMonitor) listen() {
	defer rm.wg.Done()
	wg := sync.WaitGroup{}
	defer wg.Wait() // 等待所有回调执行完成
	for {
		select {
		case t := <-rm.sd.GetTimerCbChannel():
			if t == nil {
				continue
			}
			name := t.GetName()
			wg.Add(1)
			if err := asynclib.Go(func() {
				defer wg.Done()
				if err := t.Do(); err != nil {
					log.SysLogger.Errorf("rpc monitor: %s callback failed,error:%s", name, err)
				}
			}); err != nil {
				log.SysLogger.Errorf("rpc monitor execute timeout callback failed,error:%s", err)
			}
		case <-rm.ctx.Done():
			return
		}
	}
}

func (rm *RpcMonitor) GenSeq() uint64 {
	// TODO 这个可能需要存库,否则重启的时候会有冲突的风险
	return atomic.AddUint64(&rm.seed, 1)
}

func (rm *RpcMonitor) Add(envelope inf.IEnvelope) {
	rm.locker.Lock()
	defer rm.locker.Unlock()

	timerId, err := rm.sd.AfterFunc(envelope.GetMeta().GetTimeout(), "rpc monitor", func(tm *timingwheel.Timer, args ...interface{}) error {
		elp := args[0].(inf.IEnvelope)
		if !elp.IsRef() || elp.GetMeta().GetTimerId() != tm.GetTimerId() {
			return nil
		}
		reqId := elp.GetMeta().GetReqId()
		rm.locker.Lock()
		// 直接删除
		_, ok := rm.waitMap[reqId]
		delete(rm.waitMap, reqId)
		rm.locker.Unlock()
		if !ok {
			// 已经在其他地方被移除了,不再执行后续的超时
			return nil
		}

		if elp == nil || !elp.IsRef() {
			log.SysLogger.WithContext(elp.GetContext()).Errorf("call seq is not find,seq:%d", tm.GetTimerId())
			return nil
		}

		log.SysLogger.WithContext(elp.GetContext()).Debugf("RPC call takes more than %d seconds,method is %s", int64(elp.GetMeta().GetTimeout().Seconds()), envelope.GetData().GetMethod())
		// 调用超时,执行超时回调
		rm.callTimeout(elp)
		return nil
	}, envelope)
	if err != nil {
		log.SysLogger.WithContext(envelope.GetContext()).Errorf("add monitor failed,error:%s", err)
		return
	}
	envelope.GetMeta().SetTimerId(timerId)
	rm.waitMap[envelope.GetMeta().GetReqId()] = envelope
}

func (rm *RpcMonitor) remove(seqId uint64) inf.IEnvelope {
	envelope, ok := rm.waitMap[seqId]
	if !ok {
		return nil
	}

	rm.sd.CancelTimer(envelope.GetMeta().GetTimerId())
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
	//if !envelope.IsRef() {
	//	//log.SysLogger.WithCtx(envelope.GetContext()).Debug("envelope is not ref")
	//	return // 已经被释放,丢弃
	//}

	envelope.GetData().SetResponse(nil)
	envelope.GetData().SetError(def.ErrRPCCallTimeout)

	if envelope.GetMeta().NeedCallback() {
		if err := envelope.GetMeta().GetDispatcher().PostMessage(envelope); err != nil {
			envelope.Release()
			log.SysLogger.WithContext(envelope.GetContext()).Errorf("send call timeout response error:%s", err.Error())
		}
	} else {
		envelope.SetDone()
	}
}

func (rm *RpcMonitor) NewCancel(seqId uint64) dto.CancelRpc {
	return func() {
		rm.Remove(seqId)
	}
}

func (rm *RpcMonitor) NewMultiCancel(seqIds ...uint64) dto.CancelRpc {
	return func() {
		rm.locker.Lock()
		defer rm.locker.Unlock()
		for _, seqId := range seqIds {
			if seqId == 0 {
				continue
			}
			_ = rm.remove(seqId)
		}
	}
}

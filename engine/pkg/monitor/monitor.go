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
	"time"

	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/asynclib"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
)

var rpcMonitor *RpcMonitor

type RpcMonitor struct {
	closed  atomic.Bool
	ctx     context.Context
	cancel  context.CancelFunc
	locker  sync.RWMutex
	epoch   uint64 // 启动纳秒时间戳左移44位，作为ID高位前缀（周期约1ms，不可能重启冲突）
	seq     uint64 // 自增序列号（低44位，支持约339天@60万QPS）
	waitMap map[uint64]*CallState
	sd      timingwheel.ITimerScheduler
	wg      sync.WaitGroup
}

func GetRpcMonitor() *RpcMonitor {
	if rpcMonitor == nil {
		rpcMonitor = &RpcMonitor{}
	}
	return rpcMonitor
}

func (rm *RpcMonitor) Init() *RpcMonitor {
	ctx, cancel := context.WithCancel(context.Background())
	rm.ctx = ctx
	rm.cancel = cancel
	rm.closed.Store(false)
	// 高20位: 纳秒时间戳低20位（周期约1ms，不可能在同一纳秒重启）
	// 低44位: 序列号，2^44 / 60万QPS ≈ 339天
	rm.epoch = uint64(time.Now().UnixNano()&0xFFFFF) << 44
	rm.seq = 0
	rm.waitMap = make(map[uint64]*CallState)
	rm.sd = timingwheel.NewJobScheduler("rpc monitor", config.Conf.NodeConf.RpcMonitorConf.MonitorTimerSize, config.Conf.NodeConf.RpcMonitorConf.MonitorBucketSize,
		timingwheel.GetTimingWheel(), log.NewLoggerX(log.SysLogger, log.Fields{"component": "rpc monitor"}), config.IsDebug())
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
	// 1) Stop the listen loop / scheduler first to prevent new timeout callbacks racing.
	rm.cancel()
	if rm.sd != nil {
		rm.sd.Stop()
	}
	// 2) Drain any remaining waiting states so callers don't hang and pooled states don't leak.
	var pending []*CallState
	rm.locker.Lock()
	if len(rm.waitMap) > 0 {
		pending = make([]*CallState, 0, len(rm.waitMap))
		for seq, st := range rm.waitMap {
			if st != nil {
				pending = append(pending, st)
				// Best-effort cancel: scheduler might already be stopped.
				if rm.sd != nil {
					rm.sd.CancelTimer(st.timerId())
				}
			}
			delete(rm.waitMap, seq)
		}
	}
	rm.locker.Unlock()

	for _, st := range pending {
		// Make the failure explicit; unblocks Call() waiters and triggers AsyncCall callbacks.
		st.SetResult(nil, def.ErrRPCHadClosed)
		st.Complete()
	}

	// 3) Wait for listen goroutine to exit.
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

const seqMask = uint64(0xFFFFFFFFFFF) // 低44位掩码

func (rm *RpcMonitor) GenSeq() uint64 {
	// 高20位是启动纳秒时间戳，低44位是自增序列
	// 重启后纳秒时间戳不同，ID自然不会冲突
	seq := atomic.AddUint64(&rm.seq, 1) & seqMask
	if seq == 0 {
		// seq溢出归零（总共约17.6万亿个数,除以qps*86400=可循环天数），刷新epoch避免ID冲突
		rm.epoch = uint64(time.Now().UnixNano()&0xFFFFF) << 44
	}
	return rm.epoch | seq
}

func (rm *RpcMonitor) Add(state *CallState) {
	rm.locker.Lock()
	defer rm.locker.Unlock()

	reqId := state.ReqID()
	timerId, err := rm.sd.AfterFunc(state.Timeout(), "rpc monitor", func(tm *timingwheel.Timer, args ...interface{}) error {
		seq := args[0].(uint64)
		rm.locker.Lock()
		st, ok := rm.waitMap[seq]
		if !ok || st == nil || st.timerId() != tm.GetTimerId() {
			rm.locker.Unlock()
			return nil
		}
		delete(rm.waitMap, seq)
		rm.locker.Unlock()

		if log.SysLogger != nil {
			log.SysLogger.WithContext(st.GetContext()).Debugf("RPC call takes more than %d seconds,method is %s",
				int64(st.Timeout().Seconds()), st.Method())
		}
		rm.callTimeout(st)
		return nil
	}, reqId)
	if err != nil {
		if log.SysLogger != nil {
			log.SysLogger.WithContext(state.GetContext()).Errorf("add monitor failed,error:%s", err)
		}
		// 无法加入 monitor：避免 Call 永久阻塞 / AsyncCall 永远不回调。
		state.SetResult(nil, err)
		if state.NeedCallback() {
			state.dispatchCallbackEvent()
			return
		}
		state.signalDone()
		return
	}
	state.setTimerID(timerId)
	rm.waitMap[reqId] = state
}

func (rm *RpcMonitor) remove(seqId uint64) *CallState {
	state, ok := rm.waitMap[seqId]
	if !ok {
		return nil
	}

	rm.sd.CancelTimer(state.timerId())
	delete(rm.waitMap, seqId)
	return state
}

func (rm *RpcMonitor) Remove(seqId uint64) *CallState {
	if seqId == 0 {
		return nil
	}
	rm.locker.Lock()
	f := rm.remove(seqId)
	rm.locker.Unlock()
	return f
}

func (rm *RpcMonitor) Get(seqId uint64) *CallState {
	rm.locker.RLock()
	defer rm.locker.RUnlock()

	return rm.waitMap[seqId]
}

func (rm *RpcMonitor) callTimeout(state *CallState) {
	state.SetResult(nil, def.ErrRPCCallTimeout)
	if state.NeedCallback() {
		state.dispatchCallbackEvent()
		return
	}
	state.signalDone()
}

func (rm *RpcMonitor) NewCancel(seqId uint64) dto.CancelRpc {
	return func() {
		state := rm.Remove(seqId)
		if state != nil {
			state.callbacks = nil
			state.cbParams = nil
			state.Release()
		}
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
			state := rm.remove(seqId)
			if state != nil {
				state.callbacks = nil
				state.cbParams = nil
				state.Release()
			}
		}
	}
}

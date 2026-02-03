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

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/asynclib"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
	"github.com/njtc406/emberengine/engine/pkg/utils/util"
)

var rpcMonitor *RpcMonitor
var monitorOnce sync.Once

type waitBucket struct {
	mu sync.RWMutex
	m  map[uint64]*CallState
}

type RpcMonitor struct {
	closed     atomic.Bool
	ctx        context.Context
	cancel     context.CancelFunc
	epoch      uint64 // 启动纳秒时间戳左移44位，作为ID高位前缀（周期约1ms，不可能重启冲突）
	seq        uint64 // 自增序列号（低44位，支持约339天@60万QPS）
	buckets    []waitBucket
	bucketMask uint64
	sd         timingwheel.ITimerScheduler
	wg         sync.WaitGroup
}

func (rm *RpcMonitor) bucketIndex(seqId uint64) int {
	return int(seqId & rm.bucketMask)
}

func (rm *RpcMonitor) bucket(seqId uint64) *waitBucket {
	return &rm.buckets[rm.bucketIndex(seqId)]
}

const defaultWaitBucketCount = 256

func (rm *RpcMonitor) initBuckets(bucketCount int, initCap int) {
	if bucketCount <= 0 {
		bucketCount = defaultWaitBucketCount
	}
	bucketCount = util.RoundUpToPowerOfTwoInt(bucketCount)
	rm.buckets = make([]waitBucket, bucketCount)
	for i := range rm.buckets {
		if initCap > 0 {
			rm.buckets[i].m = make(map[uint64]*CallState, initCap)
		} else {
			rm.buckets[i].m = make(map[uint64]*CallState)
		}
	}
	rm.bucketMask = uint64(bucketCount - 1)
}

func GetRpcMonitor() *RpcMonitor {
	monitorOnce.Do(func() {
		rpcMonitor = &RpcMonitor{}
		rpcMonitor.Init()
	})
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
	// Buckets are sharded to reduce lock contention.
	// BucketCount must be power-of-two; otherwise we round up.
	conf := config.Conf.NodeConf.RpcMonitorConf
	bucketCount := defaultWaitBucketCount
	if conf != nil && conf.WaitBucketCount > 0 {
		bucketCount = conf.WaitBucketCount
	}
	initCap := 0
	if conf != nil {
		initCap = conf.WaitBucketInitCap
		if initCap <= 0 {
			// Auto derive a reasonable per-bucket capacity to avoid frequent map growth.
			if conf.MonitorTimerSize > 0 {
				bc := util.RoundUpToPowerOfTwoInt(bucketCount)
				initCap = conf.MonitorTimerSize / bc
				if initCap < 16 {
					initCap = 16
				}
			}
		}
	}
	rm.initBuckets(bucketCount, initCap)
	rm.sd = timingwheel.NewJobScheduler("rpc_monitor", config.Conf.NodeConf.RpcMonitorConf.MonitorTimerSize,
		config.Conf.NodeConf.RpcMonitorConf.MonitorBucketSize,
		nil, log.NewLoggerX(log.SysLogger, log.Fields{"component": "rpc monitor"}), config.IsDebug())
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
	for i := range rm.buckets {
		b := &rm.buckets[i]
		b.mu.Lock()
		if len(b.m) > 0 {
			if pending == nil {
				pending = make([]*CallState, 0, len(b.m))
			}
			for seq, st := range b.m {
				if st != nil {
					pending = append(pending, st)
					// Best-effort cancel: scheduler might already be stopped.
					if rm.sd != nil {
						rm.sd.CancelTimer(st.timerId())
					}
				}
				delete(b.m, seq)
			}
		}
		b.mu.Unlock()
	}

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
				if err := t.Do(rm.ctx); err != nil {
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
	reqId := state.ReqID()
	timerId, err := rm.sd.AfterFunc(state.Timeout(), "rpc monitor", func(ctx context.Context, tm *timingwheel.Timer, args ...interface{}) error {
		seq := args[0].(uint64)
		st := rm.Remove(seq)
		if st == nil {
			// 已经删除
			return nil
		}

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
	b := rm.bucket(reqId)
	b.mu.Lock()
	b.m[reqId] = state
	b.mu.Unlock()
}

func (rm *RpcMonitor) removeLocked(b *waitBucket, seqId uint64) *CallState {
	state, ok := b.m[seqId]
	if !ok {
		return nil
	}
	if rm.sd != nil {
		rm.sd.CancelTimer(state.timerId())
	}
	delete(b.m, seqId)
	return state
}

func (rm *RpcMonitor) Remove(seqId uint64) *CallState {
	if seqId == 0 {
		return nil
	}
	b := rm.bucket(seqId)
	b.mu.Lock()
	f := rm.removeLocked(b, seqId)
	b.mu.Unlock()
	return f
}

func (rm *RpcMonitor) Get(seqId uint64) *CallState {
	if seqId == 0 {
		return nil
	}
	b := rm.bucket(seqId)
	b.mu.RLock()
	st := b.m[seqId]
	b.mu.RUnlock()
	return st
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
		for _, seqId := range seqIds {
			if seqId == 0 {
				continue
			}
			state := rm.Remove(seqId)
			if state != nil {
				state.callbacks = nil
				state.cbParams = nil
				state.Release()
			}
		}
	}
}

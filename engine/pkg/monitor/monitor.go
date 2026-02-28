// Package monitor
// @Title  rpc调用监视器
// @Description  用于监控rpc的call调用,当超时发生时自动回调,防止一直阻塞
// @Author  pc  2024/11/6
// @Update  pc  2024/11/6
package monitor

import (
	"context"
	"fmt"
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

type waitBucket struct {
	mu sync.RWMutex
	m  map[uint64]*CallState
}

func (w *waitBucket) Get(seq uint64) *CallState {
	w.mu.Lock()
	defer w.mu.Unlock()
	state, ok := w.m[seq]
	if ok {
		return state
	}
	return nil
}

func (w *waitBucket) Add(seq uint64, state *CallState) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.m[seq] = state
}

func (w *waitBucket) Del(seq uint64) *CallState {
	w.mu.Lock()
	defer w.mu.Unlock()
	state, ok := w.m[seq]
	delete(w.m, seq)
	if ok {
		return state
	}
	return nil
}

func (w *waitBucket) Clear() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, state := range w.m {
		state.Release()
	}
	clear(w.m)
}

type RpcMonitor struct {
	log.ILoggerX // 持有 ILoggerX
	closed       atomic.Bool
	ctx          context.Context
	cancel       context.CancelFunc
	epoch        uint64 // 启动纳秒时间戳左移44位，作为ID高位前缀（周期约1ms，不可能重启冲突）
	seq          uint64 // 自增序列号（低44位，支持约339天@60万QPS）
	buckets      []*waitBucket
	bucketMask   uint64
	sd           timingwheel.ITimerScheduler
	wg           sync.WaitGroup
	pool         *asynclib.Pool // 用于异步回调投递
}

// NewRpcMonitor 创建新的 RpcMonitor 实例。
// 每个 Node 持有独立的 RpcMonitor，互不干扰。
func NewRpcMonitor() *RpcMonitor {
	return &RpcMonitor{}
}

func (rm *RpcMonitor) bucketIndex(seqId uint64) int {
	return int(seqId & rm.bucketMask)
}

func (rm *RpcMonitor) bucket(seqId uint64) *waitBucket {
	return rm.buckets[rm.bucketIndex(seqId)]
}

const defaultWaitBucketCount = 256

func (rm *RpcMonitor) initBuckets(bucketCount int, initCap int) {
	if bucketCount <= 0 {
		bucketCount = defaultWaitBucketCount
	}
	bucketCount = util.RoundUpToPowerOfTwoInt(bucketCount)
	rm.buckets = make([]*waitBucket, bucketCount)
	for i := range rm.buckets {
		if initCap > 0 {
			rm.buckets[i] = &waitBucket{m: make(map[uint64]*CallState, initCap)}
		} else {
			rm.buckets[i] = &waitBucket{m: make(map[uint64]*CallState)}
		}
	}
	rm.bucketMask = uint64(bucketCount - 1)
}

func (rm *RpcMonitor) Init(conf *config.RpcMonitorConf, logger log.ILoggerX, tw *timingwheel.TimingWheel, pool *asynclib.Pool) *RpcMonitor {
	rm.ILoggerX = logger
	rm.pool = pool
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
	timerSize := 10000
	bucketSize := 20
	if conf != nil {
		if conf.MonitorTimerSize > 0 {
			timerSize = conf.MonitorTimerSize
		}
		if conf.MonitorBucketSize > 0 {
			bucketSize = conf.MonitorBucketSize
		}
	}
	var schedulerLogger log.ILoggerX
	if rm.ILoggerX != nil {
		schedulerLogger = rm.ILoggerX.WithField("component", "rpc monitor")
	}
	var schedulerErr error
	rm.sd, schedulerErr = timingwheel.NewJobScheduler("rpc_monitor", timerSize, bucketSize,
		tw, schedulerLogger)
	if schedulerErr != nil {
		if rm.ILoggerX != nil {
			rm.Errorf("rpc monitor create scheduler failed: %v", schedulerErr)
		}
	}
	return rm
}

func (rm *RpcMonitor) Start() error {
	if rm.closed.Load() {
		return nil
	}
	if rm.sd == nil {
		return fmt.Errorf("rpc monitor is not initialized")
	}
	rm.wg.Add(1)
	go rm.listen()
	return nil
}

func (rm *RpcMonitor) Stop() {
	if !rm.closed.CompareAndSwap(false, true) {
		return
	}
	// 节点关闭时，所有 service 已停止，无需处理 pending 调用，直接释放资源
	rm.cancel()
	if rm.sd != nil {
		rm.sd.Stop()
	}
	rm.wg.Wait()
	// 清空 buckets，释放 CallState
	for _, bucket := range rm.buckets {
		bucket.Clear()
	}
}

func (rm *RpcMonitor) listen() {
	defer rm.wg.Done()
	if rm.sd == nil {
		return
	}
	ch := rm.sd.GetTimerCbChannel()
	wg := sync.WaitGroup{}
	defer func() {
		rm.Infof("rpc monitor listen stop")
	}()
	defer wg.Wait() // 等待所有回调执行完成
	for {
		select {
		case t, ok := <-ch:
			if !ok {
				return
			}
			if t == nil {
				continue
			}
			name := t.GetName()
			wg.Add(1)
			if err := rm.pool.Go(func() {
				defer wg.Done()
				if err := t.Do(rm.ctx); err != nil {
					rm.Errorf("rpc monitor: %s callback failed,error:%s", name, err)
				}
			}); err != nil {
				wg.Done() // pool.Go 失败时也要 Done，避免 wg 泄漏
				rm.Errorf("rpc monitor execute timeout callback failed,error:%s", err)
			}
		case <-rm.ctx.Done():
			return
		}
	}
}

func (rm *RpcMonitor) isClosed() bool {
	return rm.closed.Load()
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
	if rm.isClosed() {
		state.SetResult(nil, def.ErrRPCHadClosed)
		state.Complete()
		return
	}
	reqId := state.ReqID()
	timeout := state.Timeout()
	method := state.Method()
	// TODO 这里可以直接使用异步timer,但是需要评估性能,因为现在使用的是线程池
	timerId, err := rm.sd.AfterFunc(timeout, "rpc monitor", func(_ context.Context, tm *timingwheel.Timer, args ...interface{}) error {
		defer func() {
			if rm.ILoggerX != nil {
				rm.WithContext(state.ctx).Debugf("RPC call takes more than %v seconds,method is %s",
					timeout.Milliseconds(), method)
			}
		}()
		seqId := args[0].(uint64)
		st := rm.remove(seqId) // 这里只需要移除monitor,不需要取消timer,timer已经触发了
		if st == nil {
			// 已经删除
			return nil
		}

		st.SetResult(nil, def.ErrRPCCallTimeout)
		st.Complete()
		return nil
	}, reqId)
	if err != nil {
		if rm.ILoggerX != nil {
			rm.WithContext(state.ctx).Errorf("add monitor failed,error:%s", err)
		}
		// 无法加入 monitor：避免 Call 永久阻塞 / AsyncCall 永远不回调。
		state.SetResult(nil, err)
		state.Complete()
		return
	}
	state.setTimerID(timerId)
	b := rm.bucket(reqId)
	b.Add(reqId, state)
}

func (rm *RpcMonitor) remove(seqId uint64) *CallState {
	if rm.isClosed() {
		return nil
	}
	if seqId == 0 {
		return nil
	}
	b := rm.bucket(seqId)
	return b.Del(seqId)
}

func (rm *RpcMonitor) Remove(seqId uint64) *CallState {
	state := rm.remove(seqId)
	if state != nil {
		if rm.sd != nil && !rm.isClosed() {
			rm.sd.CancelTimer(state.timerId())
		}
	}
	return state
}

func (rm *RpcMonitor) Get(seqId uint64) *CallState {
	if rm.isClosed() {
		return nil
	}
	if seqId == 0 {
		return nil
	}
	b := rm.bucket(seqId)
	return b.Get(seqId)
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

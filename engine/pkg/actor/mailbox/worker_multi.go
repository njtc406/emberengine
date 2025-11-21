// Package mailbox
// @Title  多优先级队列工作线程
// @Description  desc
// @Author  yr  2025/2/8
// @Update  yr  2025/2/8
package mailbox

import (
	"fmt"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"reflect"
	"runtime/debug"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/profiler"
	"github.com/njtc406/emberengine/engine/pkg/utils/mpsc"
)

type MultiWorker struct {
	workerId int
	closed   atomic.Bool
	config   *config.MultiLevelMailboxConf
	pool     *WorkerPool
	wg       sync.WaitGroup

	priorityQueues map[def.Priority]queue[inf.IEvent] // 多级优先级队列
	scheduler      *PriorityScheduler                 // 优先级调度器

	// 事件驱动通知机制 - 优化：使用原子计数器避免信号丢失
	mutex          sync.Mutex
	cond           *sync.Cond
	pendingSignals atomic.Int64 // 待处理信号计数器，避免消息丢失

	// 性能优化：预计算的批次限制和优先级排序列表
	totalBatchLimit    int                  // 预计算的总批次限制
	sortedPriorities   []def.Priority       // 预排序的优先级列表（数值越小优先级越高）
	priorityBatchSizes map[def.Priority]int // 预计算的批次大小映射

	// 非空队列跟踪优化
	nonEmptyQueues map[def.Priority]struct{} // 非空队列集合，避免全量扫描
	nonEmptyMutex  sync.RWMutex              // 保护非空队列集合的锁

	// 内存复用优化
	availablePrioritiesPool sync.Pool // 用于复用 availablePriorities 切片

	// 时间片公平调度机制（防止低优先级饿死）
	timeSliceTracker    map[def.Priority]*timeSliceInfo // 每个优先级的时间片信息
	lastLowPriorityTime atomic.Int64                    // 上次处理低优先级消息的时间（纳秒）
	fairnessThresholdNs int64                           // 公平性阈值（纳秒）
}

func (w *MultiWorker) Stop() {
	// 标记为已关闭
	w.closed.Store(true)

	// 唤醒所有等待中的 goroutine
	w.cond.Broadcast()

	// 等待所有 goroutine 完成
	w.wg.Wait()
}

// timeSliceInfo 时间片信息
type timeSliceInfo struct {
	lastProcessedTime atomic.Int64 // 上次处理时间（纳秒）
	processedCount    atomic.Int64 // 在当前时间片内处理的消息数
	timeSliceBudget   int64        // 时间片预算（纳秒）
}

// calculateTimeSliceBudget 根据优先级计算时间片预算（纳秒）
func (w *MultiWorker) calculateTimeSliceBudget(priority def.Priority) int64 {
	// 根据优先级分配不同的时间片：优先级越高，时间片越大
	switch {
	case priority <= def.PriorityHigh: // 高优先级
		return 50_000_000 // 50ms
	case priority == def.PriorityNormal: // 普通优先级
		return 30_000_000 // 30ms
	case priority == def.PriorityLow: // 低优先级
		return 20_000_000 // 20ms
	default: // 后台优先级
		return 10_000_000 // 10ms
	}
}

func newMultiWorker(workerId int, conf *config.WorkerConf, pool *WorkerPool) inf.IMailboxWorker {
	if conf.MultiLevelConf == nil {
		conf.MultiLevelConf = DefaultWorkerConfig()
	}

	w := &MultiWorker{
		workerId:            workerId,
		config:              conf.MultiLevelConf,
		pool:                pool,
		priorityQueues:      make(map[def.Priority]queue[inf.IEvent]),
		priorityBatchSizes:  make(map[def.Priority]int),
		nonEmptyQueues:      make(map[def.Priority]struct{}),
		timeSliceTracker:    make(map[def.Priority]*timeSliceInfo),
		fairnessThresholdNs: 100_000_000, // 默认100ms公平性阈值
	}
	w.cond = sync.NewCond(&w.mutex)

	// 初始化内存池用于复用切片
	w.availablePrioritiesPool.New = func() interface{} {
		return make([]def.Priority, 0, 16) // 预分配容量以减少扩容
	}

	// 初始化多级优先级系统（必须启用）
	w.scheduler = NewPriorityScheduler(conf.MultiLevelConf)

	// 为每个优先级创建队列并预计算优化数据
	totalBatchSize := 0
	for lv, pc := range conf.MultiLevelConf.PriorityBatches {
		w.priorityQueues[lv] = mpsc.New[inf.IEvent]()
		w.priorityBatchSizes[lv] = pc.BatchSize
		totalBatchSize += pc.BatchSize
		w.sortedPriorities = append(w.sortedPriorities, lv)

		// 初始化时间片信息（根据优先级高低分配不同的时间片）
		timeSliceBudgetNs := w.calculateTimeSliceBudget(lv)
		w.timeSliceTracker[lv] = &timeSliceInfo{
			timeSliceBudget: timeSliceBudgetNs,
		}
	}

	// 预排序优先级列表（数值越小优先级越高）
	sort.Slice(w.sortedPriorities, func(i, j int) bool {
		return w.sortedPriorities[i] < w.sortedPriorities[j]
	})

	// 设置预计算的总批次限制
	if totalBatchSize <= 0 {
		w.totalBatchLimit = 32 // 默认值
	} else {
		const maxReasonableBatch = 128
		if totalBatchSize > maxReasonableBatch {
			w.totalBatchLimit = maxReasonableBatch
		} else {
			w.totalBatchLimit = totalBatchSize
		}
	}

	return w
}

// SubmitEvent 提交事件（修复版：确保非空队列集合正确更新）
func (w *MultiWorker) SubmitEvent(e inf.IEvent) error {
	// 检查Worker是否已关闭（双重检查）
	if w.closed.Load() {
		return def.ErrWorkerClosed
	}

	// 检查优先级是否有效
	priority := e.GetPriority()
	que, exists := w.priorityQueues[priority]
	if !exists {
		return fmt.Errorf("invalid priority: %d", priority)
	}

	// 再次检查Worker状态（关键：在获取队列后再次检查）
	if w.closed.Load() {
		return def.ErrWorkerClosed
	}

	// 空->非空边沿唤醒，降低等待时间
	emptyBefore := que.Empty()
	// 提交消息到对应的优先级队列
	que.Push(e)

	// 最后检查：只有在Worker未关闭时才发送信号
	if !w.closed.Load() {
		if emptyBefore {
			w.signalNewMessage() // 立即唤醒
		} else {
			w.signalNewMessageSafely() // 合并信号
		}
	}
	return nil
}

// GetWorkerId 获取Worker的ID
func (w *MultiWorker) GetWorkerId() int {
	return w.workerId
}

// SubmitEventWithPriority 提交指定优先级的事件
func (w *MultiWorker) SubmitEventWithPriority(e inf.IEvent, priority def.Priority) error {
	// 设置事件的优先级
	xctx, ok := e.(interface{ SetHeader(string, any) })
	if ok {
		xctx.SetHeader(def.DefaultPriorityKey, priority)
	}

	// 使用现有的SubmitEvent方法处理
	return w.SubmitEvent(e)
}

func (w *MultiWorker) Start() {
	w.wg.Add(1)
	go w.run()
}

func (w *MultiWorker) run() {
	defer w.wg.Done()

	// busy-wait 模式，接近旧版mailbox1路径（无条件变量）
	if w.config != nil && w.config.WaitMode == "busy" {
		w.runBusy()
		return
	}

	var e inf.IEvent
	var ok bool

	defer func() {
		// 退出时处理所有剩余消息
		for priority, que := range w.priorityQueues {
			for !que.Empty() {
				if e, ok = que.Pop(); ok {
					w.safeExecMultiLevel(e, priority)
				}
			}
		}
	}()

	for !w.closed.Load() {
		// 阻塞等待新消息通知
		w.waitForNewMessages()

		// 被唤醒后处理所有可用消息（但先检查是否关闭）
		for !w.closed.Load() {
			processedAny := w.processAvailableMessages()
			if !processedAny {
				break // 没有更多消息，回到等待状态
			}
			// 继续处理，直到没有消息为止
		}
	}
}

// runBusy 忙等处理循环（微睡眠退避），避免条件变量唤醒开销
func (w *MultiWorker) runBusy() {
	backoff := 1
	const maxBackoff = 8
	for !w.closed.Load() {
		if processed := w.processAvailableMessages(); !processed {
			if backoff < maxBackoff {
				backoff <<= 1
			}
			time.Sleep(time.Microsecond * time.Duration(backoff))
		} else {
			backoff = 1
		}
	}
}

func (w *MultiWorker) stop() {
	//log.SysLogger.Debugf("worker %d process lowPriCount:%d  highPriCount:%d", w.workerId, w.lowPriCount.Load(), w.highPriCount.Load())
	// 首先设置关闭标志，阻止新的 SubmitEvent
	if w.closed.Swap(true) {
		return // 已经关闭过了
	}

	// 使用条件变量唤醒worker（但不会处理新消息）
	w.signalNewMessage()
	w.wg.Wait()

	// 清理资源，但不置空mailbox为nil，避免panic风险
	w.pool = nil
	w.workerId = 0
	w.config = nil

	// 清理多级优先级相关资源（优化：保留空壳避免panic）
	if w.scheduler != nil {
		w.scheduler = nil
		// 保留空的 map 结构，不删除键，避免 SubmitEvent 在 race 下 panic
		// 只清空预排序列表和非空集合
		w.sortedPriorities = w.sortedPriorities[:0]
		w.nonEmptyMutex.Lock()
		for k := range w.nonEmptyQueues {
			delete(w.nonEmptyQueues, k)
		}
		w.nonEmptyMutex.Unlock()

		// 重置信号计数器
		w.pendingSignals.Store(0)
	}
}

// signalNewMessage 安全地通知worker有新消息（强制发送）
func (w *MultiWorker) signalNewMessage() {
	w.mutex.Lock()
	w.pendingSignals.Store(1) // 强制设置信号
	w.cond.Signal()           // 唤醒等待的goroutine
	w.mutex.Unlock()
}

// signalNewMessageSafely 安全发送信号，避免重复信号（优化版）
func (w *MultiWorker) signalNewMessageSafely() {
	// 使用原子操作增加信号计数
	if w.pendingSignals.Add(1) == 1 {
		// 只有在从 0 变为 1 时才发送信号，避免重复唤醒
		w.mutex.Lock()
		w.cond.Signal()
		w.mutex.Unlock()
	}
}

// waitForNewMessages 使用原子计数器等待新消息（修复版：避免信号丢失和死锁）
func (w *MultiWorker) waitForNewMessages() {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	// 使用for循环防止虚假唤醒，检查是否有待处理信号
	for w.pendingSignals.Load() == 0 && !w.closed.Load() {
		w.cond.Wait()
	}

	// 醒来后消耗一个信号（如果有的话）
	// 使用循环CAS，但要确保不会死锁
	for {
		current := w.pendingSignals.Load()
		if current <= 0 {
			// 没有信号可消耗（可能被其他地方消耗或虚假唤醒），直接返回
			break
		}
		// 尝试将信号计数减1
		if w.pendingSignals.CompareAndSwap(current, current-1) {
			// 成功消耗一个信号
			break
		}
		// CAS失败，说明有并发修改，重试
		// 注意：重新检查current是否>0，避免无限循环
	}
}

// processAvailableMessages 处理当前可用的消息（高性能优化版本）
func (w *MultiWorker) processAvailableMessages() bool {
	processedAny := false
	totalProcessed := 0

	// 使用预计算的总批次限制
	maxBatchTotal := w.totalBatchLimit

	// 从对象池获取可复用的切片
	availablePrioritySlice := w.availablePrioritiesPool.Get().([]def.Priority)
	availablePriorities := availablePrioritySlice[:0] // 重置长度但保留容量
	defer func() {
		// 归还对象到池中
		w.availablePrioritiesPool.Put(availablePriorities[:0])
	}()

	// 收集非空优先级（简单扫描，避免锁竞争）
	for _, priority := range w.sortedPriorities {
		if !w.priorityQueues[priority].Empty() {
			availablePriorities = append(availablePriorities, priority)
		}
	}
	// 没有消息可处理
	if len(availablePriorities) == 0 {
		return false
	}
	// 记录当前时间用于时间片统计
	currentTime := time.Now().UnixNano()

	// 如果只有一个非空优先级队列，走快路径，避免排序/策略选择
	if len(availablePriorities) == 1 {
		selectedPriority := availablePriorities[0]
		que := w.priorityQueues[selectedPriority]
		for totalProcessed < maxBatchTotal {
			remainingBatch := maxBatchTotal - totalProcessed
			processedCount := 0
			// 在忙等模式下使用批量弹出以提升吞吐，否则逐条 Pop 以控制GC
			if w.config != nil && w.config.WaitMode == "busy" {
				batchLimit := w.priorityBatchSizes[selectedPriority]
				if batchLimit > remainingBatch {
					batchLimit = remainingBatch
				}
				batch := que.BatchPop(batchLimit)
				for _, e := range batch {
					w.safeExecMultiLevel(e, selectedPriority)
					processedAny = true
					processedCount++
				}
			} else {
				// 使用逐条 Pop（避免 BatchPop 大切片分配导致 GC 抖动）
				for i := 0; i < remainingBatch; i++ {
					if e, ok := que.Pop(); ok {
						w.safeExecMultiLevel(e, selectedPriority)
						processedAny = true
						processedCount++
					} else {
						break
					}
				}
			}
			totalProcessed += processedCount
			w.updateTimeSliceInfo(selectedPriority, currentTime, int64(processedCount))
			if processedCount == 0 || que.Empty() {
				break
			}
		}
		return processedAny
	}
	for totalProcessed < maxBatchTotal && len(availablePriorities) > 0 {
		// 检查是否需要强制处理低优先级消息（防止饿死）
		selectedPriority := w.scheduler.NextPriorityWithOrdering(availablePriorities)
		if selectedPriority == -1 {
			break
		}

		// 获取对应的队列和批量大小（使用预计算值）
		que := w.priorityQueues[selectedPriority]
		batchSize := w.priorityBatchSizes[selectedPriority]

		// 限制本次批量大小，避免超过总批次限制
		remainingBatch := maxBatchTotal - totalProcessed
		if batchSize > remainingBatch {
			batchSize = remainingBatch
		}

		// 批量处理该优先级的消息（使用BatchPop减少原子操作）
		batch := que.BatchPop(batchSize)
		processedCount := 0
		for _, e := range batch {
			w.safeExecMultiLevel(e, selectedPriority)
			processedAny = true
			processedCount++
		}
		// 队列可能已空，必要时从可用列表中移除
		if processedCount < batchSize && que.Empty() {
			w.removeFromAvailable(&availablePriorities, selectedPriority)
		}

		// 更新总处理计数和时间片信息
		totalProcessed += processedCount
		w.updateTimeSliceInfo(selectedPriority, currentTime, int64(processedCount))

		// 如果这次没有处理任何消息，退出循环
		if processedCount == 0 {
			// 快路径：队列空了，移除并继续
			w.removeFromAvailable(&availablePriorities, selectedPriority)
			break
		}
	}

	return processedAny
}

// getBatchSizeForPriority 获取指定优先级的批量大小（使用预计算值）
func (w *MultiWorker) getBatchSizeForPriority(priority def.Priority) int {
	if batchSize, ok := w.priorityBatchSizes[priority]; ok {
		return batchSize
	}
	return 8 // 默认值
}

// sortAvailablePriorities 高效排序可用优先级（优化版：避免内存分配）
func (w *MultiWorker) sortAvailablePriorities(available []def.Priority) {
	// 使用基于预排序列表的排序算法，复杂度O(n)，无内存分配
	if len(available) <= 1 {
		return
	}

	// 原地排序：遍历预排序列表，将匹配的优先级依次放置到available前面
	writeIndex := 0
	for _, sortedPriority := range w.sortedPriorities {
		// 在available中查找匹配的优先级
		for readIndex := writeIndex; readIndex < len(available); readIndex++ {
			if available[readIndex] == sortedPriority {
				// 找到匹配，交换到writeIndex位置
				if readIndex != writeIndex {
					available[writeIndex], available[readIndex] = available[readIndex], available[writeIndex]
				}
				writeIndex++
				break
			}
		}
	}
}

// removeFromAvailable 从可用列表中移除指定优先级
func (w *MultiWorker) removeFromAvailable(available *[]def.Priority, priority def.Priority) {
	slice := *available
	for i, p := range slice {
		if p == priority {
			// 快速移除：将最后一个元素移动到当前位置
			slice[i] = slice[len(slice)-1]
			*available = slice[:len(slice)-1]
			return
		}
	}
}

// safeExecMultiLevel 安全执行多级优先级消息（增强错误处理）
func (w *MultiWorker) safeExecMultiLevel(e inf.IEvent, priority def.Priority) {
	defer func() {
		if r := recover(); r != nil {
			log.SysLogger.WithContext(e.GetContext()).Errorf("exec multi-level def.Priority event error (def.Priority=%d): %v\ntrace:%s", priority, r, debug.Stack())

			// 双重保护：EscalateFailure 可能也会 panic
			func() {
				defer func() {
					if r2 := recover(); r2 != nil {
						log.SysLogger.WithContext(e.GetContext()).Errorf("EscalateFailure also panicked: %v", r2)
					}
				}()
				w.pool.invoker.EscalateFailure(r, e)
			}()
		}
	}()

	var analyzer *profiler.Analyzer
	if w.pool.profiler != nil {
		analyzer = w.pool.profiler.Push(fmt.Sprintf("[ STATE-P%d ]%s", priority, reflect.TypeOf(e).String()))
	}

	// 调用消息处理器
	w.pool.invoker.InvokeMessage(e)

	if analyzer != nil {
		analyzer.Pop() // 记录分析日志
		analyzer = nil
	}

	for _, ms := range w.pool.middlewares {
		ms.MessageReceived(e)
	}
}

// GetMsgLen 获取所有队列的总长度（更新后的接口）
func (w *MultiWorker) GetMsgLen() int {
	total := 0
	for _, que := range w.priorityQueues {
		total += que.Len()
	}
	return total
}

// GetPriorityQueueLen 获取指定优先级队列的长度
func (w *MultiWorker) GetPriorityQueueLen(priority def.Priority) int {
	que, exists := w.priorityQueues[priority]
	if !exists {
		return 0
	}
	return que.Len()
}

// GetTotalQueueLen 获取所有队列的总长度
func (w *MultiWorker) GetTotalQueueLen() int {
	return w.GetMsgLen()
}

// GetPriorityStatistics 获取优先级统计信息（增强版）
func (w *MultiWorker) GetPriorityStatistics() map[def.Priority]map[string]interface{} {
	stats := make(map[def.Priority]map[string]interface{})
	for priority, que := range w.priorityQueues {
		priorityStats := map[string]interface{}{
			"queue_length": int64(que.Len()),
			"batch_size":   int64(w.getBatchSizeForPriority(priority)),
		}

		// 添加时间片相关统计
		if timeSlice, exists := w.timeSliceTracker[priority]; exists {
			priorityStats["time_slice_budget_ns"] = timeSlice.timeSliceBudget
			priorityStats["last_processed_time"] = timeSlice.lastProcessedTime.Load()
			priorityStats["processed_count"] = timeSlice.processedCount.Load()
			currentTime := time.Now().UnixNano()
			lastTime := timeSlice.lastProcessedTime.Load()
			if lastTime > 0 {
				priorityStats["time_since_last_processed_ms"] = (currentTime - lastTime) / 1_000_000
			}
		}

		stats[priority] = priorityStats
	}
	return stats
}

// GetSchedulerStatistics 获取调度器统计信息（新增）
func (w *MultiWorker) GetSchedulerStatistics() map[string]interface{} {
	if w.scheduler == nil {
		return nil
	}
	return map[string]interface{}{
		"strategy":          string(w.scheduler.strategy),
		"priorities":        len(w.scheduler.priorities),
		"total_batch_limit": w.totalBatchLimit,
		"sorted_priorities": w.sortedPriorities,
	}
}

// selectPriorityWithFairness 基于公平性和时间片选择优先级
func (w *MultiWorker) selectPriorityWithFairness(availablePriorities []def.Priority, currentTime int64) def.Priority {
	// 检查是否需要强制处理低优先级消息（防止饿死）
	lastLowPriorityTime := w.lastLowPriorityTime.Load()
	if lastLowPriorityTime > 0 && (currentTime-lastLowPriorityTime) > w.fairnessThresholdNs {
		// 超过公平性阈值，强制选择低优先级消息
		for _, priority := range availablePriorities {
			if priority >= def.PriorityLow {
				w.lastLowPriorityTime.Store(currentTime)
				return priority
			}
		}
	}

	// 正常调度：使用时间片机制选择优先级
	return w.selectPriorityByTimeSlice(availablePriorities, currentTime)
}

// selectPriorityByTimeSlice 基于时间片机制选择优先级
func (w *MultiWorker) selectPriorityByTimeSlice(availablePriorities []def.Priority, currentTime int64) def.Priority {
	// 优先使用调度器的正常选择
	schedulerSelected := w.scheduler.NextPriorityWithOrdering(availablePriorities)
	if schedulerSelected == -1 {
		return -1
	}

	// 检查选中的优先级是否超过时间片限制
	timeSlice, exists := w.timeSliceTracker[schedulerSelected]
	if !exists {
		return schedulerSelected
	}

	lastProcessedTime := timeSlice.lastProcessedTime.Load()
	if lastProcessedTime == 0 || (currentTime-lastProcessedTime) > timeSlice.timeSliceBudget {
		// 时间片已过期或是第一次处理，可以处理
		return schedulerSelected
	}

	// 时间片未过期，尝试选择其他优先级
	for _, priority := range availablePriorities {
		if priority == schedulerSelected {
			continue
		}
		timeSlice, exists := w.timeSliceTracker[priority]
		if !exists {
			return priority
		}
		lastProcessedTime := timeSlice.lastProcessedTime.Load()
		if lastProcessedTime == 0 || (currentTime-lastProcessedTime) > timeSlice.timeSliceBudget {
			return priority
		}
	}

	// 所有优先级都在时间片内，返回调度器选择的优先级
	return schedulerSelected
}

// updateTimeSliceInfo 更新时间片信息
func (w *MultiWorker) updateTimeSliceInfo(priority def.Priority, currentTime int64, processedCount int64) {
	timeSlice, exists := w.timeSliceTracker[priority]
	if !exists {
		return
	}

	timeSlice.lastProcessedTime.Store(currentTime)
	timeSlice.processedCount.Add(processedCount)

	// 如果处理的是低优先级消息，更新全局记录
	if priority >= def.PriorityLow {
		w.lastLowPriorityTime.Store(currentTime)
	}
}

// GetFairnessStatistics 获取公平性统计信息
func (w *MultiWorker) GetFairnessStatistics() map[string]interface{} {
	currentTime := time.Now().UnixNano()
	lastLowPriorityTime := w.lastLowPriorityTime.Load()

	stats := map[string]interface{}{
		"fairness_threshold_ms":  w.fairnessThresholdNs / 1_000_000,
		"last_low_priority_time": lastLowPriorityTime,
	}

	if lastLowPriorityTime > 0 {
		stats["time_since_last_low_priority_ms"] = (currentTime - lastLowPriorityTime) / 1_000_000
		stats["is_starvation_risk"] = (currentTime - lastLowPriorityTime) > w.fairnessThresholdNs
	} else {
		stats["time_since_last_low_priority_ms"] = 0
		stats["is_starvation_risk"] = false
	}

	return stats
}

// GetWorkerHealthStatus 获取Worker健康状态
func (w *MultiWorker) GetWorkerHealthStatus() map[string]interface{} {
	totalQueueLength := w.GetTotalQueueLen()
	pendingSignals := w.pendingSignals.Load()
	currentTime := time.Now().UnixNano()

	status := map[string]interface{}{
		"worker_id":          w.workerId,
		"is_closed":          w.closed.Load(),
		"total_queue_length": totalQueueLength,
		"pending_signals":    pendingSignals,
		"current_time_ns":    currentTime,
		"total_batch_limit":  w.totalBatchLimit,
	}

	// 添加非空队列信息
	w.nonEmptyMutex.RLock()
	nonEmptyCount := len(w.nonEmptyQueues)
	nonEmptyPriorities := make([]def.Priority, 0, nonEmptyCount)
	for priority := range w.nonEmptyQueues {
		nonEmptyPriorities = append(nonEmptyPriorities, priority)
	}
	w.nonEmptyMutex.RUnlock()

	status["non_empty_queue_count"] = nonEmptyCount
	status["non_empty_priorities"] = nonEmptyPriorities

	return status
}

// GetDetailedStatistics 获取详细的综合统计信息
func (w *MultiWorker) GetDetailedStatistics() map[string]interface{} {
	return map[string]interface{}{
		"priority_statistics":  w.GetPriorityStatistics(),
		"scheduler_statistics": w.GetSchedulerStatistics(),
		"fairness_statistics":  w.GetFairnessStatistics(),
		"health_status":        w.GetWorkerHealthStatus(),
	}
}

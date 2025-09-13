// Package mailbox
// @Title  服务的工作线程,接收并处理事件
// @Description  desc
// @Author  yr  2025/2/8
// @Update  yr  2025/2/8
package mailbox

import (
	"fmt"
	"reflect"
	"runtime/debug"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/profiler"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/mpsc"
)

type Worker struct {
	workerId int
	closed   atomic.Bool
	config   *WorkerConfig
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
}

func newWorker(pool *WorkerPool, id int, config *WorkerConfig) *Worker {
	if config == nil {
		config = DefaultWorkerConfig()
	}

	w := &Worker{
		workerId:           id,
		config:             config,
		pool:               pool,
		priorityQueues:     make(map[def.Priority]queue[inf.IEvent]),
		priorityBatchSizes: make(map[def.Priority]int),
		nonEmptyQueues:     make(map[def.Priority]struct{}),
	}
	w.cond = sync.NewCond(&w.mutex)

	// 初始化内存池用于复用切片
	w.availablePrioritiesPool.New = func() interface{} {
		return make([]def.Priority, 0, 16) // 预分配容量以减少扩容
	}

	// 初始化多级优先级系统（必须启用）
	var prioritiesConfig map[def.Priority]PriorityConfig
	if config.MultiLevel != nil && config.MultiLevel.Enabled {
		w.scheduler = NewPriorityScheduler(config.MultiLevel)
		prioritiesConfig = config.MultiLevel.Priorities
	} else {
		// 如果没有配置多级优先级，使用默认配置
		defaultConfig := &MultiLevelConfig{
			Enabled:    true,
			Strategy:   def.StrategyAbsolute,
			Priorities: newDefaultPriorityMap(),
		}
		w.scheduler = NewPriorityScheduler(defaultConfig)
		prioritiesConfig = defaultConfig.Priorities
	}

	// 为每个优先级创建队列并预计算优化数据
	totalBatchSize := 0
	for lv, pc := range prioritiesConfig {
		w.priorityQueues[lv] = mpsc.New[inf.IEvent]()
		w.priorityBatchSizes[lv] = pc.BatchSize
		totalBatchSize += pc.BatchSize
		w.sortedPriorities = append(w.sortedPriorities, lv)
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

// SubmitEvent 提交事件（优化版：简化信号逻辑并添加非空队列跟踪）
func (w *Worker) SubmitEvent(e inf.IEvent) error {
	// 检查Worker是否已关闭
	if w.closed.Load() {
		return def.ErrWorkerClosed
	}

	// 检查优先级是否有效
	priority := e.GetPriority()
	que, exists := w.priorityQueues[priority]
	if !exists {
		return fmt.Errorf("invalid priority: %d", priority)
	}

	// 提交消息到对应的优先级队列
	wasEmpty := que.Empty()
	que.Push(e)

	// 如果队列从空变为非空，添加到非空队列集合
	if wasEmpty {
		w.nonEmptyMutex.Lock()
		w.nonEmptyQueues[priority] = struct{}{}
		w.nonEmptyMutex.Unlock()
	}

	// 简化信号逻辑：直接发送信号，由 signalNewMessageSafely 保证不重复
	w.signalNewMessageSafely()
	return nil
}

func (w *Worker) Start() {
	w.wg.Add(1)
	go w.run()
}

func (w *Worker) run() {
	defer w.wg.Done()

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

	// 真正的事件驱动循环：等待→处理→等待
	for !w.closed.Load() {
		// 阻塞等待新消息通知
		w.waitForNewMessages()

		// 被唤醒后处理所有可用消息
		for {
			processedAny := w.processAvailableMessages()
			if !processedAny {
				break // 没有更多消息，回到等待状态
			}
			// 继续处理，直到没有消息为止
		}
	}
}

func (w *Worker) stop() {
	//log.SysLogger.Debugf("worker %d process lowPriCount:%d  highPriCount:%d", w.workerId, w.lowPriCount.Load(), w.highPriCount.Load())
	if w.closed.Swap(true) {
		return
	}
	// 使用条件变量唤醒worker
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
func (w *Worker) signalNewMessage() {
	w.mutex.Lock()
	w.pendingSignals.Store(1) // 强制设置信号
	w.cond.Signal()           // 唤醒等待的goroutine
	w.mutex.Unlock()
}

// signalNewMessageSafely 安全发送信号，避免重复信号（优化版）
func (w *Worker) signalNewMessageSafely() {
	// 使用原子操作增加信号计数
	if w.pendingSignals.Add(1) == 1 {
		// 只有在从 0 变为 1 时才发送信号，避免重复唤醒
		w.mutex.Lock()
		w.cond.Signal()
		w.mutex.Unlock()
	}
}

// waitForNewMessages 使用原子计数器等待新消息（优化版：避免信号丢失）
func (w *Worker) waitForNewMessages() {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	// 使用for循环防止虚假唤醒，检查是否有待处理信号
	for w.pendingSignals.Load() == 0 && !w.closed.Load() {
		w.cond.Wait()
	}

	// 消耗一个信号（使用CAS保证原子性）
	for {
		current := w.pendingSignals.Load()
		if current <= 0 {
			break
		}
		// 尝试将信号计数减1，如果成功则退出
		if w.pendingSignals.CompareAndSwap(current, current-1) {
			break
		}
		// CAS失败，重试
	}
}

// processAvailableMessages 处理当前可用的消息（高性能优化版本）
func (w *Worker) processAvailableMessages() bool {
	var e inf.IEvent
	var ok bool
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

	// 优化：优先从非空队列集合中收集，避免全量扫描
	w.nonEmptyMutex.RLock()
	for priority := range w.nonEmptyQueues {
		if w.priorityQueues[priority].Len() > 0 {
			availablePriorities = append(availablePriorities, priority)
		} else {
			// 队列已空，从非空集合中移除（延迟删除）
			delete(w.nonEmptyQueues, priority)
		}
	}
	w.nonEmptyMutex.RUnlock()

	// 如果没有非空队列，返回
	if len(availablePriorities) == 0 {
		return false
	}

	// 按照预排序的优先级列表进行排序（高效版本）
	w.sortAvailablePriorities(availablePriorities)

	// 公平化调度,在总批次限制内轮流处理不同优先级
	for totalProcessed < maxBatchTotal && len(availablePriorities) > 0 {
		// 使用改进的调度器进行两阶段选择
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

		// 批量处理该优先级的消息
		processedCount := 0
		for i := 0; i < batchSize; i++ {
			if e, ok = que.Pop(); ok {
				w.safeExecMultiLevel(e, selectedPriority)
				processedAny = true
				processedCount++
			} else {
				// 队列已空，从可用列表中移除
				w.removeFromAvailable(&availablePriorities, selectedPriority)
				break
			}
		}

		// 更新总处理计数
		totalProcessed += processedCount

		// 如果这次没有处理任何消息，退出循环
		if processedCount == 0 {
			break
		}
	}

	return processedAny
}

// getBatchSizeForPriority 获取指定优先级的批量大小（使用预计算值）
func (w *Worker) getBatchSizeForPriority(priority def.Priority) int {
	if batchSize, ok := w.priorityBatchSizes[priority]; ok {
		return batchSize
	}
	return 8 // 默认值
}

// sortAvailablePriorities 高效排序可用优先级（基于预排序列表）
func (w *Worker) sortAvailablePriorities(available []def.Priority) {
	// 使用基于预排序列表的排序算法，复杂度O(n)
	if len(available) <= 1 {
		return
	}

	// 构建可用优先级的集合
	availableSet := make(map[def.Priority]bool, len(available))
	for _, p := range available {
		availableSet[p] = true
	}

	// 按预排序列表重新排列
	result := available[:0] // 重用切片
	for _, p := range w.sortedPriorities {
		if availableSet[p] {
			result = append(result, p)
		}
	}

	// 复制回原切片
	copy(available, result)
}

// removeFromAvailable 从可用列表中移除指定优先级
func (w *Worker) removeFromAvailable(available *[]def.Priority, priority def.Priority) {
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
func (w *Worker) safeExecMultiLevel(e inf.IEvent, priority def.Priority) {
	defer func() {
		if r := recover(); r != nil {
			log.SysLogger.Errorf("exec multi-level def.Priority event error (def.Priority=%d): %v\ntrace:%s", priority, r, debug.Stack())

			// 双重保护：EscalateFailure 可能也会 panic
			func() {
				defer func() {
					if r2 := recover(); r2 != nil {
						log.SysLogger.Errorf("EscalateFailure also panicked: %v", r2)
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

	// TODO 这里实际上不需要区分最后的invoker了，都是需要处理的消息,所以本身并没有不同,只是调度的优先级问题
	// 根据优先级选择合适的调用方式
	// 这里可以根据业务需要定制不同优先级的处理逻辑
	if priority <= 0 {
		// 高优先级消息使用系统消息处理器
		w.pool.invoker.InvokeSystemMessage(e)
	} else {
		// 低优先级消息使用用户消息处理器
		w.pool.invoker.InvokeUserMessage(e)
	}

	if analyzer != nil {
		analyzer.Pop() // 记录分析日志
		analyzer = nil
	}

	for _, ms := range w.pool.middlewares {
		ms.MessageReceived(e)
	}
}

// GetMsgLen 获取所有队列的总长度（更新后的接口）
func (w *Worker) GetMsgLen() int {
	total := 0
	for _, que := range w.priorityQueues {
		total += que.Len()
	}
	return total
}

// GetPriorityQueueLen 获取指定优先级队列的长度
func (w *Worker) GetPriorityQueueLen(priority def.Priority) int {
	que, exists := w.priorityQueues[priority]
	if !exists {
		return 0
	}
	return que.Len()
}

// GetTotalQueueLen 获取所有队列的总长度
func (w *Worker) GetTotalQueueLen() int {
	return w.GetMsgLen()
}

// GetPriorityStatistics 获取优先级统计信息（新增）
func (w *Worker) GetPriorityStatistics() map[def.Priority]map[string]int64 {
	stats := make(map[def.Priority]map[string]int64)
	for priority, que := range w.priorityQueues {
		stats[priority] = map[string]int64{
			"queue_length": int64(que.Len()),
			"batch_size":   int64(w.getBatchSizeForPriority(priority)),
		}
	}
	return stats
}

// GetSchedulerStatistics 获取调度器统计信息（新增）
func (w *Worker) GetSchedulerStatistics() map[string]interface{} {
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

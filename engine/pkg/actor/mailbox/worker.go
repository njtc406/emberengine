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
	"sync"
	"sync/atomic"
	"time"

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

	lowPriMailbox  queue[inf.IEvent] // 低优先级消息
	highPriMailbox queue[inf.IEvent] // 高优先级消息
	lowPriCount    atomic.Int64      // 低优先级消息计数
	highPriCount   atomic.Int64      // 高优先级消息计数

	// 多级优先级支持
	multiLevelQueues    map[def.Priority]queue[inf.IEvent] // 多级优先级队列
	multiLevelCount     map[def.Priority]*atomic.Int64     // 多级消息计数器
	multiLevelProcessed map[def.Priority]*atomic.Int64     // 多级已处理计数器
	lastProcessedTime   map[def.Priority]*atomic.Int64     // 最后处理时间
	scheduler           *PriorityScheduler                 // 优先级调度器

	// 事件驱动通知机制
	mutex         sync.Mutex
	cond          *sync.Cond
	hasNewMessage bool // 标记是否有新消息
}

func newWorker(pool *WorkerPool, id int, config *WorkerConfig) *Worker {
	if config == nil {
		config = DefaultWorkerConfig()
	}
	w := &Worker{
		workerId:            id,
		config:              config,
		pool:                pool,
		lowPriMailbox:       mpsc.New[inf.IEvent](),
		highPriMailbox:      mpsc.New[inf.IEvent](),
		multiLevelQueues:    make(map[def.Priority]queue[inf.IEvent]),
		multiLevelCount:     make(map[def.Priority]*atomic.Int64),
		multiLevelProcessed: make(map[def.Priority]*atomic.Int64),
		lastProcessedTime:   make(map[def.Priority]*atomic.Int64),
	}
	w.cond = sync.NewCond(&w.mutex)

	// 初始化多级优先级支持
	if config.MultiLevel != nil && config.MultiLevel.Enabled {
		w.scheduler = NewPriorityScheduler(config.MultiLevel)

		// 为每个优先级创建队列和计数器
		for _, pc := range config.MultiLevel.Priorities {
			w.multiLevelQueues[pc.Level] = mpsc.New[inf.IEvent]()
			w.multiLevelCount[pc.Level] = &atomic.Int64{}
			w.multiLevelProcessed[pc.Level] = &atomic.Int64{}
			w.lastProcessedTime[pc.Level] = &atomic.Int64{}
		}
	}

	return w
}

func (w *Worker) submitLowPriEvent(e inf.IEvent) error {
	// 检查Worker是否已关闭
	if w.closed.Load() {
		return def.ErrWorkerClosed
	}
	if w.lowPriMailbox == nil {
		return def.ErrMailboxWorkerUserChannelNotInit
	}

	// 获取Push前的队列长度
	oldLength := w.lowPriMailbox.Len()

	// Push消息到队列
	w.lowPriMailbox.Push(e)
	w.lowPriCount.Add(1)

	// 只有在队列从空变为非空时才发送信号
	if oldLength == 0 {
		// 队列从空变为非空，需要唤醒worker
		w.signalNewMessage()
	}
	return nil
}

func (w *Worker) submitHighPriEvent(e inf.IEvent) error {
	// 检查Worker是否已关闭
	if w.closed.Load() {
		return def.ErrWorkerClosed
	}
	if w.highPriMailbox == nil {
		return def.ErrMailboxWorkerSysChannelNotInit
	}

	// 获取Push前的队列长度
	oldLength := w.highPriMailbox.Len()

	// Push消息到队列
	w.highPriMailbox.Push(e)
	w.highPriCount.Add(1)

	// 只有在队列从空变为非空时才发送信号
	if oldLength == 0 {
		// 队列从空变为非空，需要唤醒worker
		w.signalNewMessage()
	}
	return nil
}

// ======== 多级优先级队列 ========

// SubmitEventWithPriority 提交指定优先级的事件
func (w *Worker) SubmitEventWithPriority(e inf.IEvent, priority def.Priority) error {
	// 检查Worker是否已关闭
	if w.closed.Load() {
		return def.ErrWorkerClosed
	}

	// 检查是否在多级模式下
	if w.scheduler == nil {
		// 非多级模式，回退到传统高/低优先级
		if priority <= 0 {
			return w.submitHighPriEvent(e)
		} else {
			return w.submitLowPriEvent(e)
		}
	}

	// 检查优先级是否有效
	que, exists := w.multiLevelQueues[priority]
	if !exists {
		return fmt.Errorf("invalid def.Priority level: %d", priority)
	}

	// 提交消息到对应的优先级队列
	countCounter := w.multiLevelCount[priority]

	oldLength := que.Len()

	que.Push(e)
	countCounter.Add(1)

	if oldLength == 0 {
		w.signalNewMessage()
	}
	return nil
}

func (w *Worker) Start() {
	w.wg.Add(1)
	go w.run()
}

func (w *Worker) run() {
	//log.SysLogger.Debugf("worker %d start", w.workerId)
	defer w.wg.Done()

	var e inf.IEvent
	var ok bool

	defer func() {
		// 退出时检查业务是否处理完成
		// 先处理传统队列
		for !w.highPriMailbox.Empty() {
			if e, ok = w.highPriMailbox.Pop(); ok {
				w.safeExec(w.pool.invoker.InvokeSystemMessage, e)
			}
		}

		for !w.lowPriMailbox.Empty() {
			if e, ok = w.lowPriMailbox.Pop(); ok {
				w.safeExec(w.pool.invoker.InvokeUserMessage, e)
			}
		}

		// 处理多级优先级队列
		if w.scheduler != nil {
			for priority, que := range w.multiLevelQueues {
				for !que.Empty() {
					if e, ok = que.Pop(); ok {
						w.safeExecMultiLevel(e, priority)
						// 更新处理计数器
						w.multiLevelProcessed[priority].Add(1)
						// 更新最后处理时间
						w.lastProcessedTime[priority].Store(time.Now().Unix())
					}
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
	//log.SysLogger.Debugf("worker %d stopped", w.workerId)
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

	// 清理多级优先级相关资源
	if w.scheduler != nil {
		w.scheduler = nil
		// 清空多级队列map，但不置为nil
		for k := range w.multiLevelQueues {
			delete(w.multiLevelQueues, k)
		}
		for k := range w.multiLevelCount {
			delete(w.multiLevelCount, k)
		}
		for k := range w.multiLevelProcessed {
			delete(w.multiLevelProcessed, k)
		}
		for k := range w.lastProcessedTime {
			delete(w.lastProcessedTime, k)
		}
	}
}

// signalNewMessage 安全地通知worker有新消息
func (w *Worker) signalNewMessage() {
	w.mutex.Lock()
	w.hasNewMessage = true
	w.cond.Signal() // 唤醒等待的goroutine
	w.mutex.Unlock()
}

// waitForNewMessages 使用条件变量等待新消息
func (w *Worker) waitForNewMessages() {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	// 使用for循环防止虚假唤醒，确保条件变量的正确性
	for !w.hasNewMessage && !w.closed.Load() {
		w.cond.Wait()
	}
	w.hasNewMessage = false
}

// processAvailableMessages 处理当前可用的消息
func (w *Worker) processAvailableMessages() bool {
	// 检查是否在多级模式下
	if w.scheduler != nil {
		return w.processMultiLevelMessages()
	}

	// 传统的高/低优先级处理模式
	return w.processLegacyMessages()
}

// processMultiLevelMessages 处理多级优先级消息
func (w *Worker) processMultiLevelMessages() bool {
	var e inf.IEvent
	var ok bool
	processedAny := false

	// 设置总批次上限，避免单优先级占用过久
	maxBatchTotal := w.getTotalBatchLimit()
	totalProcessed := 0

	// 公平化调度,在总批次限制内轮流处理不同优先级
	for totalProcessed < maxBatchTotal {
		// 获取所有有消息的优先级
		availablePriorities := make([]def.Priority, 0, len(w.multiLevelQueues))
		for priority, queue := range w.multiLevelQueues {
			if queue.Len() > 0 {
				availablePriorities = append(availablePriorities, priority)
			}
		}

		// 如果没有可用的优先级，退出
		if len(availablePriorities) == 0 {
			break
		}

		// 使用调度器选择下一个处理的优先级
		selectedPriority := w.scheduler.NextPriority(availablePriorities)
		if selectedPriority == -1 {
			break
		}

		// 获取对应的队列和批量大小
		que := w.multiLevelQueues[selectedPriority]
		batchSize := w.getBatchSizeForPriority(selectedPriority)

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
				break
			}
		}

		// 更新处理计数器和最后处理时间
		if processedCount > 0 {
			// 更新独立的处理计数器
			w.multiLevelProcessed[selectedPriority].Add(int64(processedCount))
			// 更新最后处理时间
			w.lastProcessedTime[selectedPriority].Store(time.Now().Unix())
			totalProcessed += processedCount
		}

		// 如果这次没有处理任何消息，说明队列可能已空，退出循环
		if processedCount == 0 {
			break
		}
	}

	return processedAny
}

// getTotalBatchLimit 获取总批次限制
func (w *Worker) getTotalBatchLimit() int {
	// 计算配置的所有优先级批量大小之和
	totalLimit := 0
	if w.config.MultiLevel != nil {
		for _, pc := range w.config.MultiLevel.Priorities {
			totalLimit += pc.BatchSize
		}
	}

	// 采用更合理的默认值策略
	if totalLimit <= 0 {
		// 如果没有配置，使用保守的默认值
		return 32
	}

	// 使用min(配置的总和, 128)，避免过大的批量导致单次处理时间过长
	// 同时避免过小的限制导致低优先级几乎得不到机会
	const maxReasonableBatch = 128
	if totalLimit > maxReasonableBatch {
		return maxReasonableBatch
	}

	return totalLimit
}

// processLegacyMessages 处理传统的高/低优先级消息
func (w *Worker) processLegacyMessages() bool {
	var e inf.IEvent
	var ok bool
	processedAny := false

	// 优先批量处理高优先级消息
	maxHighPriBatch := w.config.HighPriBatch
	for i := 0; i < maxHighPriBatch; i++ {
		if e, ok = w.highPriMailbox.Pop(); ok {
			w.safeExec(w.pool.invoker.InvokeSystemMessage, e)
			processedAny = true
		} else {
			break
		}
	}

	// 2. 批量处理低优先级消息（数量限制，避免高优先级消息被饥饿）
	maxLowPriBatch := w.config.LowPriBatch
	for i := 0; i < maxLowPriBatch; i++ {
		if e, ok = w.lowPriMailbox.Pop(); ok {
			w.safeExec(w.pool.invoker.InvokeUserMessage, e)
			processedAny = true
		} else {
			break
		}
	}

	return processedAny
}

// getBatchSizeForPriority 获取指定优先级的批量大小
func (w *Worker) getBatchSizeForPriority(priority def.Priority) int {
	// 从配置中查找对应的批量大小
	for _, pc := range w.config.MultiLevel.Priorities {
		if pc.Level == priority {
			return pc.BatchSize
		}
	}
	// 默认批量大小
	return 8
}

// safeExecMultiLevel 安全执行多级优先级消息
func (w *Worker) safeExecMultiLevel(e inf.IEvent, priority def.Priority) {
	defer func() {
		if r := recover(); r != nil {
			log.SysLogger.Errorf("exec multi-level def.Priority event error (def.Priority=%d): %v\ntrace:%s", priority, r, debug.Stack())
			w.pool.invoker.EscalateFailure(r, e)
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

func (w *Worker) safeExec(invokeFun func(inf.IEvent), e inf.IEvent) {
	defer func() {
		if r := recover(); r != nil {
			log.SysLogger.Errorf("exec error: %v\ntrace:%s", r, debug.Stack())
			w.pool.invoker.EscalateFailure(r, e)
		}
	}()

	var analyzer *profiler.Analyzer
	if w.pool.profiler != nil {
		analyzer = w.pool.profiler.Push(fmt.Sprintf("[ STATE ]%s", reflect.TypeOf(e).String()))
	}
	invokeFun(e)
	if analyzer != nil {
		analyzer.Pop()
		analyzer = nil
	}

	for _, ms := range w.pool.middlewares {
		ms.MessageReceived(e)
	}
}

func (w *Worker) GetMsgLen() int {
	if w.lowPriMailbox == nil {
		return 0
	}
	return w.lowPriMailbox.Len()
}

// GetPriorityQueueLen 获取指定优先级队列的长度
func (w *Worker) GetPriorityQueueLen(priority def.Priority) int {
	if w.scheduler == nil {
		return 0
	}
	que, exists := w.multiLevelQueues[priority]
	if !exists {
		return 0
	}
	return que.Len()
}

// GetTotalQueueLen 获取所有队列的总长度
func (w *Worker) GetTotalQueueLen() int {
	total := w.GetMsgLen()
	if w.highPriMailbox != nil {
		total += w.highPriMailbox.Len()
	}
	if w.scheduler != nil {
		for _, que := range w.multiLevelQueues {
			total += que.Len()
		}
	}
	return total
}

// GetPriorityStatistics 获取优先级统计信息
func (w *Worker) GetPriorityStatistics() map[def.Priority]map[string]int64 {
	if w.scheduler == nil {
		return nil
	}

	stats := make(map[def.Priority]map[string]int64)
	for priority, queue := range w.multiLevelQueues {
		stats[priority] = map[string]int64{
			"queue_length":      int64(queue.Len()),                         // 使用queue内建长度计数器
			"total_count":       w.multiLevelCount[priority].Load(),         // 总接收计数器
			"processed_count":   w.multiLevelProcessed[priority].Load(),     // 已处理计数器
			"batch_size":        int64(w.getBatchSizeForPriority(priority)), // 批量大小
			"last_processed_at": w.lastProcessedTime[priority].Load(),       // 最后处理时间
		}
	}
	return stats
}

// GetSchedulerStatistics 获取调度器统计信息
func (w *Worker) GetSchedulerStatistics() map[string]interface{} {
	if w.scheduler == nil {
		return nil
	}

	w.scheduler.mutex.RLock()
	defer w.scheduler.mutex.RUnlock()

	stats := map[string]interface{}{
		"strategy":   string(w.scheduler.strategy),
		"priorities": len(w.scheduler.priorities),
		"counters":   make(map[def.Priority]int),
		"weights":    make(map[def.Priority]int),
	}

	// 复制计数器和权重信息
	for p, counter := range w.scheduler.counters {
		stats["counters"].(map[def.Priority]int)[p] = counter
	}
	for p, weight := range w.scheduler.weights {
		stats["weights"].(map[def.Priority]int)[p] = weight
	}

	return stats
}

// TODO 直接替换为新接口
// ======== 向后兼容的方法 ========
// 保持原有接口不变，内部调用新的优先级方法

// submitUserEvent 向后兼容方法，内部调用 submitLowPriEvent
func (w *Worker) submitUserEvent(e inf.IEvent) error {
	return w.submitLowPriEvent(e)
}

// submitSysEvent 向后兼容方法，内部调用 submitHighPriEvent
func (w *Worker) submitSysEvent(e inf.IEvent) error {
	return w.submitHighPriEvent(e)
}

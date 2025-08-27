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

// Priority 优先级级别类型
type Priority int

// 预定义的优先级常量（数值越小优先级越高）
const (
	PriorityUrgent     Priority = -2 // 紧急优先级
	PriorityHigh       Priority = -1 // 高优先级（与传统高优先级相对应）
	PriorityNormal     Priority = 0  // 普通优先级
	PriorityLow        Priority = 1  // 低优先级（与传统低优先级相对应）
	PriorityBatch      Priority = 2  // 批量处理优先级
	PriorityBackground Priority = 3  // 后台优先级
)

// ScheduleStrategy 调度策略
type ScheduleStrategy string

const (
	StrategyAbsolute ScheduleStrategy = "absolute" // 绝对优先策略
	StrategyWeighted ScheduleStrategy = "weighted" // 加权轮询策略
	StrategyFairness ScheduleStrategy = "fairness" // 防饥饿策略
)

// PriorityConfig 单个优先级配置
type PriorityConfig struct {
	Level     Priority `json:"level"`      // 优先级级别（数字越小优先级越高）
	BatchSize int      `json:"batch_size"` // 该级别批量处理大小
	Weight    int      `json:"weight"`     // 调度权重（用于加权策略）
}

// MultiLevelConfig 多级队列配置
type MultiLevelConfig struct {
	Enabled    bool             `json:"enabled"`    // 是否启用多级队列
	Priorities []PriorityConfig `json:"priorities"` // 优先级配置列表
	Strategy   ScheduleStrategy `json:"strategy"`   // 调度策略
}

// WorkerConfig Worker配置参数
type WorkerConfig struct {
	// 简单模式配置（向后兼容）
	HighPriBatch int // 高优先级消息批量大小，默认16
	LowPriBatch  int // 低优先级消息批量大小，默认8

	// 多级队列配置（可选）
	MultiLevel *MultiLevelConfig `json:"multi_level,omitempty"`
}

// DefaultWorkerConfig 返回默认配置
func DefaultWorkerConfig() *WorkerConfig {
	return &WorkerConfig{
		HighPriBatch: 16,
		LowPriBatch:  8,
	}
}

// PriorityScheduler 多级优先级调度器
type PriorityScheduler struct {
	strategy   ScheduleStrategy
	priorities []PriorityConfig
	weights    map[Priority]int
	counters   map[Priority]int // 用于加权轮询和防饥饿
	mutex      sync.RWMutex
}

// NewPriorityScheduler 创建新的优先级调度器
func NewPriorityScheduler(config *MultiLevelConfig) *PriorityScheduler {
	if config == nil || !config.Enabled {
		return nil
	}

	scheduler := &PriorityScheduler{
		strategy:   config.Strategy,
		priorities: config.Priorities,
		weights:    make(map[Priority]int),
		counters:   make(map[Priority]int),
	}

	// 初始化权重映射
	for _, pc := range config.Priorities {
		scheduler.weights[pc.Level] = pc.Weight
		scheduler.counters[pc.Level] = 0
	}

	return scheduler
}

// NextPriority 根据调度策略返回下一个应该处理的优先级
func (ps *PriorityScheduler) NextPriority(availablePriorities []Priority) Priority {
	if ps == nil || len(availablePriorities) == 0 {
		return -1
	}

	ps.mutex.Lock()
	defer ps.mutex.Unlock()

	switch ps.strategy {
	case StrategyAbsolute:
		return ps.absolutePriority(availablePriorities)
	case StrategyWeighted:
		return ps.weightedPriority(availablePriorities)
	case StrategyFairness:
		return ps.fairnessPriority(availablePriorities)
	default:
		return ps.absolutePriority(availablePriorities)
	}
}

// absolutePriority 绝对优先策略：总是选择数值最小的优先级
func (ps *PriorityScheduler) absolutePriority(available []Priority) Priority {
	minPriority := available[0]
	for _, p := range available[1:] {
		if p < minPriority {
			minPriority = p
		}
	}
	return minPriority
}

// weightedPriority 加权轮询策略：根据权重分配处理机会
func (ps *PriorityScheduler) weightedPriority(available []Priority) Priority {
	// 检查并重置计数器（防止溢出）
	ps.resetCountersIfNeeded()

	// 找到计数器值最小且有权重的优先级
	var selectedPriority Priority = -1
	minRatio := float64(1<<63 - 1)

	for _, p := range available {
		weight, exists := ps.weights[p]
		if !exists || weight <= 0 {
			continue
		}

		counter := ps.counters[p]
		ratio := float64(counter) / float64(weight)

		if selectedPriority == -1 || ratio < minRatio {
			minRatio = ratio
			selectedPriority = p
		}
	}

	if selectedPriority != -1 {
		ps.counters[selectedPriority]++
	}

	return selectedPriority
}

// fairnessPriority 防饥饿策略：确保每个优先级都有处理机会
func (ps *PriorityScheduler) fairnessPriority(available []Priority) Priority {
	// 检查并重置计数器（防止溢出）
	ps.resetCountersIfNeeded()

	// 找到计数器值最小的优先级
	var selectedPriority Priority = -1
	minCounter := 1<<63 - 1

	for _, p := range available {
		counter := ps.counters[p]
		if selectedPriority == -1 || counter < minCounter {
			minCounter = counter
			selectedPriority = p
		}
	}

	if selectedPriority != -1 {
		ps.counters[selectedPriority]++
	}

	return selectedPriority
}

// resetCountersIfNeeded 检查并重置计数器（防止溢出）
func (ps *PriorityScheduler) resetCountersIfNeeded() {
	// 检查是否有计数器超过阈值
	const resetThreshold = 1e9
	maxCounter := 0

	for _, counter := range ps.counters {
		if counter > maxCounter {
			maxCounter = counter
		}
	}

	// 如果最大计数器超过阈值，就统一归一化
	if maxCounter > resetThreshold {
		for p := range ps.counters {
			ps.counters[p] = ps.counters[p] / 2
		}
	}
}

type Worker struct {
	workerId int
	closed   atomic.Bool
	config   *WorkerConfig
	pool     *WorkerPool
	wg       sync.WaitGroup

	// 向后兼容：保留原有字段
	lowPriMailbox  queue[inf.IEvent] // 低优先级消息
	highPriMailbox queue[inf.IEvent] // 高优先级消息
	lowPriCount    atomic.Int64      // 低优先级消息计数
	highPriCount   atomic.Int64      // 高优先级消息计数
	lowPriLength   atomic.Int64      // 低优先级队列长度（用于优化信号）
	highPriLength  atomic.Int64      // 高优先级队列长度（用于优化信号）

	// 多级优先级支持
	multiLevelQueues    map[Priority]queue[inf.IEvent] // 多级优先级队列
	multiLevelLength    map[Priority]*atomic.Int64     // 多级队列长度计数器
	multiLevelCount     map[Priority]*atomic.Int64     // 多级消息计数器
	multiLevelProcessed map[Priority]*atomic.Int64     // 多级已处理计数器（独立维护）
	lastProcessedTime   map[Priority]*atomic.Int64     // 最后处理时间（Unix时间戳）
	scheduler           *PriorityScheduler             // 优先级调度器

	// 事件驱动通知机制
	mutex         sync.Mutex
	cond          *sync.Cond
	hasNewMessage bool // 标记是否有新消息，由mutex保护
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
		multiLevelQueues:    make(map[Priority]queue[inf.IEvent]),
		multiLevelLength:    make(map[Priority]*atomic.Int64),
		multiLevelCount:     make(map[Priority]*atomic.Int64),
		multiLevelProcessed: make(map[Priority]*atomic.Int64),
		lastProcessedTime:   make(map[Priority]*atomic.Int64),
	}
	w.cond = sync.NewCond(&w.mutex)

	// 初始化多级优先级支持
	if config.MultiLevel != nil && config.MultiLevel.Enabled {
		w.scheduler = NewPriorityScheduler(config.MultiLevel)

		// 为每个优先级创建队列和计数器
		for _, pc := range config.MultiLevel.Priorities {
			w.multiLevelQueues[pc.Level] = mpsc.New[inf.IEvent]()
			w.multiLevelLength[pc.Level] = &atomic.Int64{}
			w.multiLevelCount[pc.Level] = &atomic.Int64{}
			w.multiLevelProcessed[pc.Level] = &atomic.Int64{} // 独立的处理计数器
			w.lastProcessedTime[pc.Level] = &atomic.Int64{}   // 最后处理时间
		}
	}

	return w
}

func (w *Worker) submitLowPriEvent(e inf.IEvent) error {
	// 防御性编程：检查Worker是否已关闭
	if w.closed.Load() {
		return def.ErrWorkerClosed
	}
	if w.lowPriMailbox == nil {
		return def.ErrMailboxWorkerUserChannelNotInit
	}

	w.lowPriCount.Add(1)
	if !w.lowPriMailbox.Push(e) {
		return def.ErrEventChannelIsFull
	}

	// 智能信号机制：只有在队列从空变为非空时才发送信号
	oldLength := w.lowPriLength.Add(1)
	if oldLength == 1 {
		// 队列从空变为非空，需要唤醒worker
		w.signalNewMessage()
	}
	return nil
}

func (w *Worker) submitHighPriEvent(e inf.IEvent) error {
	// 防御性编程：检查Worker是否已关闭
	if w.closed.Load() {
		return def.ErrWorkerClosed
	}
	if w.highPriMailbox == nil {
		return def.ErrMailboxWorkerSysChannelNotInit
	}

	w.highPriCount.Add(1)
	if !w.highPriMailbox.Push(e) {
		return def.ErrSysEventChannelIsFull
	}

	// 智能信号机制：只有在队列从空变为非空时才发送信号
	oldLength := w.highPriLength.Add(1)
	if oldLength == 1 {
		// 队列从空变为非空，需要唤醒worker
		w.signalNewMessage()
	}
	return nil
}

// ======== 多级优先级队列支持 ========

// SubmitEventWithPriority 提交指定优先级的事件
func (w *Worker) SubmitEventWithPriority(e inf.IEvent, priority Priority) error {
	// 防御性编程：检查Worker是否已关闭
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
	queue, exists := w.multiLevelQueues[priority]
	if !exists {
		return fmt.Errorf("invalid priority level: %d", priority)
	}

	// 提交消息到对应的优先级队列
	lengthCounter := w.multiLevelLength[priority]
	countCounter := w.multiLevelCount[priority]

	countCounter.Add(1)
	if !queue.Push(e) {
		return def.ErrEventChannelIsFull
	}

	// 智能信号机制：只有在队列从空变为非空时才发送信号
	oldLength := lengthCounter.Add(1)
	if oldLength == 1 {
		// 队列从空变为非空，需要唤醒worker
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
		// 先处理传统队列并同步更新计数器
		highPriProcessed := 0
		for !w.highPriMailbox.Empty() {
			if e, ok = w.highPriMailbox.Pop(); ok {
				w.safeExec(w.pool.invoker.InvokeSystemMessage, e)
				highPriProcessed++
			}
		}
		// 同步更新高优先级计数器
		if highPriProcessed > 0 {
			w.highPriLength.Add(-int64(highPriProcessed))
		}

		lowPriProcessed := 0
		for !w.lowPriMailbox.Empty() {
			if e, ok = w.lowPriMailbox.Pop(); ok {
				w.safeExec(w.pool.invoker.InvokeUserMessage, e)
				lowPriProcessed++
			}
		}
		// 同步更新低优先级计数器
		if lowPriProcessed > 0 {
			w.lowPriLength.Add(-int64(lowPriProcessed))
		}

		// 处理多级优先级队列并同步更新计数器
		if w.scheduler != nil {
			for priority, queue := range w.multiLevelQueues {
				lengthCounter := w.multiLevelLength[priority]
				multiLevelProcessed := 0
				for !queue.Empty() {
					if e, ok = queue.Pop(); ok {
						w.safeExecMultiLevel(e, priority)
						multiLevelProcessed++
					}
				}
				// 同步更新多级队列计数器
				if multiLevelProcessed > 0 {
					lengthCounter.Add(-int64(multiLevelProcessed))
					// 更新处理计数器
					w.multiLevelProcessed[priority].Add(int64(multiLevelProcessed))
					// 更新最后处理时间
					w.lastProcessedTime[priority].Store(time.Now().Unix())
				}
			}
		}
	}()

	// 真正的事件驱动循环：等待→处理→等待
	for !w.closed.Load() {
		// 等待新消息通知（真正阻塞等待）
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
		for k := range w.multiLevelLength {
			delete(w.multiLevelLength, k)
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
// 使用条件变量实现真正的事件驱动，避免轮询和sleep
func (w *Worker) signalNewMessage() {
	w.mutex.Lock()
	w.hasNewMessage = true
	w.cond.Signal() // 唤醒等待的goroutine
	w.mutex.Unlock()
}

// waitForNewMessages 使用条件变量等待新消息
// 实现真正的阻塞等待，避免CPU资源浪费
// 使用for循环防止条件变量的虚假唤醒
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
// 返回是否处理了任何消息
// 采用高优先级消息绝对优先 + 批量处理的策略
func (w *Worker) processAvailableMessages() bool {
	// 检查是否在多级模式下
	if w.scheduler != nil {
		return w.processMultiLevelMessages()
	}

	// 传统的高/低优先级处理模式
	return w.processLegacyMessages()
}

// processMultiLevelMessages 处理多级优先级消息
// 采用公平化批次调度，避免单一优先级长期占用worker
func (w *Worker) processMultiLevelMessages() bool {
	var e inf.IEvent
	var ok bool
	processedAny := false

	// 设置总批次上限，避免单优先级占用过久
	maxBatchTotal := w.getTotalBatchLimit()
	totalProcessed := 0

	// 公平化调度：在总批次限制内轮流处理不同优先级
	for totalProcessed < maxBatchTotal {
		// 获取所有有消息的优先级
		availablePriorities := make([]Priority, 0, len(w.multiLevelQueues))
		for priority, lengthCounter := range w.multiLevelLength {
			if lengthCounter.Load() > 0 {
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
		queue := w.multiLevelQueues[selectedPriority]
		lengthCounter := w.multiLevelLength[selectedPriority]
		batchSize := w.getBatchSizeForPriority(selectedPriority)

		// 限制本次批量大小，避免超过总批次限制
		remainingBatch := maxBatchTotal - totalProcessed
		if batchSize > remainingBatch {
			batchSize = remainingBatch
		}

		// 批量处理该优先级的消息
		processedCount := 0
		for i := 0; i < batchSize; i++ {
			if e, ok = queue.Pop(); ok {
				w.safeExecMultiLevel(e, selectedPriority)
				processedAny = true
				processedCount++
			} else {
				break
			}
		}

		// 同步更新队列长度计数器和处理计数器
		if processedCount > 0 {
			lengthCounter.Add(-int64(processedCount))
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

	// 1. 绝对优先批量处理高优先级消息
	maxHighPriBatch := w.config.HighPriBatch
	highPriProcessed := 0
	for i := 0; i < maxHighPriBatch; i++ {
		if e, ok = w.highPriMailbox.Pop(); ok {
			w.safeExec(w.pool.invoker.InvokeSystemMessage, e)
			processedAny = true
			highPriProcessed++
		} else {
			break
		}
	}
	// 同步更新高优先级队列长度计数器
	if highPriProcessed > 0 {
		w.highPriLength.Add(-int64(highPriProcessed))
	}

	// 2. 批量处理低优先级消息（数量限制，避免高优先级消息被饥饿）
	maxLowPriBatch := w.config.LowPriBatch
	lowPriProcessed := 0
	for i := 0; i < maxLowPriBatch; i++ {
		if e, ok = w.lowPriMailbox.Pop(); ok {
			w.safeExec(w.pool.invoker.InvokeUserMessage, e)
			processedAny = true
			lowPriProcessed++
		} else {
			break
		}
	}
	// 同步更新低优先级队列长度计数器
	if lowPriProcessed > 0 {
		w.lowPriLength.Add(-int64(lowPriProcessed))
	}

	return processedAny
}

// getBatchSizeForPriority 获取指定优先级的批量大小
func (w *Worker) getBatchSizeForPriority(priority Priority) int {
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
func (w *Worker) safeExecMultiLevel(e inf.IEvent, priority Priority) {
	defer func() {
		if r := recover(); r != nil {
			log.SysLogger.Errorf("exec multi-level priority event error (priority=%d): %v\ntrace:%s", priority, r, debug.Stack())
			w.pool.invoker.EscalateFailure(r, e)
		}
	}()

	var analyzer *profiler.Analyzer
	if w.pool.profiler != nil {
		analyzer = w.pool.profiler.Push(fmt.Sprintf("[ STATE-P%d ]%s", priority, reflect.TypeOf(e).String()))
	}

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
		analyzer.Pop()
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
func (w *Worker) GetPriorityQueueLen(priority Priority) int {
	if w.scheduler == nil {
		return 0
	}
	queue, exists := w.multiLevelQueues[priority]
	if !exists {
		return 0
	}
	return queue.Len()
}

// GetTotalQueueLen 获取所有队列的总长度
func (w *Worker) GetTotalQueueLen() int {
	total := w.GetMsgLen()
	if w.highPriMailbox != nil {
		total += w.highPriMailbox.Len()
	}
	if w.scheduler != nil {
		for _, queue := range w.multiLevelQueues {
			total += queue.Len()
		}
	}
	return total
}

// GetPriorityStatistics 获取优先级统计信息
func (w *Worker) GetPriorityStatistics() map[Priority]map[string]int64 {
	if w.scheduler == nil {
		return nil
	}

	stats := make(map[Priority]map[string]int64)
	for priority := range w.multiLevelQueues {
		stats[priority] = map[string]int64{
			"queue_length":      w.multiLevelLength[priority].Load(),
			"total_count":       w.multiLevelCount[priority].Load(),
			"processed_count":   w.multiLevelProcessed[priority].Load(), // 使用独立计数器，避免由于Push失败导致的不准确
			"batch_size":        int64(w.getBatchSizeForPriority(priority)),
			"last_processed_at": w.lastProcessedTime[priority].Load(), // 最后处理时间，用于检测饥饿
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
		"counters":   make(map[Priority]int),
		"weights":    make(map[Priority]int),
	}

	// 复制计数器和权重信息
	for p, counter := range w.scheduler.counters {
		stats["counters"].(map[Priority]int)[p] = counter
	}
	for p, weight := range w.scheduler.weights {
		stats["weights"].(map[Priority]int)[p] = weight
	}

	return stats
}

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

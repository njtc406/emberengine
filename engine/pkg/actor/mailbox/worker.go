// Package mailbox
// @Title  服务的工作线程,接收并处理事件
// @Description  desc
// @Author  yr  2025/2/8
// @Update  yr  2025/2/8
package mailbox

import (
	"fmt"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/profiler"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/mpsc"
	"reflect"
	"runtime/debug"
	"sort"
	"sync"
	"sync/atomic"
)

type Worker struct {
	workerId int
	closed   atomic.Bool
	config   *WorkerConfig
	pool     *WorkerPool
	wg       sync.WaitGroup

	priorityQueues map[def.Priority]queue[inf.IEvent] // 多级优先级队列
	scheduler      *PriorityScheduler                 // 优先级调度器

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
		workerId:       id,
		config:         config,
		pool:           pool,
		priorityQueues: make(map[def.Priority]queue[inf.IEvent]),
	}
	w.cond = sync.NewCond(&w.mutex)

	// 初始化多级优先级系统（必须启用）
	if config.MultiLevel != nil && config.MultiLevel.Enabled {
		w.scheduler = NewPriorityScheduler(config.MultiLevel)

		// 为每个优先级创建队列
		for lv, _ := range config.MultiLevel.Priorities {
			w.priorityQueues[lv] = mpsc.New[inf.IEvent]()
		}
	} else {
		// 如果没有配置多级优先级，使用默认配置
		defaultConfig := &MultiLevelConfig{
			Enabled:    true,
			Strategy:   def.StrategyAbsolute,
			Priorities: newDefaultPriorityMap(),
		}
		w.scheduler = NewPriorityScheduler(defaultConfig)

		// 初始化默认优先级
		for lv, _ := range defaultConfig.Priorities {
			w.priorityQueues[lv] = mpsc.New[inf.IEvent]()
		}
	}

	return w
}

// SubmitEvent 提交事件
func (w *Worker) SubmitEvent(e inf.IEvent) error {
	// 检查Worker是否已关闭
	if w.closed.Load() {
		return def.ErrWorkerClosed
	}

	// 检查优先级是否有效
	que, exists := w.priorityQueues[e.GetPriority()]
	if !exists {
		return fmt.Errorf("invalid priority: %d", e.GetPriority())
	}

	// 提交消息到对应的优先级队列
	oldLength := que.Len()
	que.Push(e)

	// 只有在队列从空变为非空时才发送信号,降低变更频率
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

	// 清理多级优先级相关资源
	if w.scheduler != nil {
		w.scheduler = nil
		// 清空多级队列map
		clear(w.priorityQueues)
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
	var e inf.IEvent
	var ok bool
	processedAny := false

	// 设置总批次上限，避免单优先级占用过久
	maxBatchTotal := w.getTotalBatchLimit()
	totalProcessed := 0

	// 公平化调度,在总批次限制内轮流处理不同优先级
	for totalProcessed < maxBatchTotal {
		// 获取所有有消息的优先级（按优先级从高到低排序）
		var availablePriorities []def.Priority
		// 收集所有有消息的优先级
		for priority, que := range w.priorityQueues {
			if que.Len() > 0 {
				availablePriorities = append(availablePriorities, priority)
			}
		}

		// 如果没有可用的优先级，退出
		if len(availablePriorities) == 0 {
			break
		}

		// 按优先级从高到低排序（数值越小优先级越高）
		sort.Slice(availablePriorities, func(i, j int) bool {
			return availablePriorities[i] < availablePriorities[j]
		})

		//if len(availablePriorities) > 1 {
		//	// 使用简单的排序算法确保高优先级在前
		//	for i := 0; i < len(availablePriorities)-1; i++ {
		//		for j := i + 1; j < len(availablePriorities); j++ {
		//			if availablePriorities[i] > availablePriorities[j] { // 数值小的优先级高
		//				availablePriorities[i], availablePriorities[j] = availablePriorities[j], availablePriorities[i]
		//			}
		//		}
		//	}
		//}

		// 使用改进的调度器进行两阶段选择：
		// 第一阶段：严格按优先级排序（已完成）
		// 第二阶段：在保证优先级的前提下，使用调度策略
		selectedPriority := w.scheduler.NextPriorityWithOrdering(availablePriorities)
		if selectedPriority == -1 {
			break
		}

		// 获取对应的队列和批量大小
		que := w.priorityQueues[selectedPriority]
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

		// 更新总处理计数
		totalProcessed += processedCount

		// 如果这次没有处理任何消息，说明队列可能已空，退出循环
		if processedCount == 0 {
			break
		}
	}

	return processedAny
}

// TODO 这个函数在初始化完之后应该就可以计算出来了，不需要每次都计算
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

// getBatchSizeForPriority 获取指定优先级的批量大小
func (w *Worker) getBatchSizeForPriority(priority def.Priority) int {
	if conf, ok := w.config.MultiLevel.Priorities[priority]; ok {
		return conf.BatchSize
	}
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

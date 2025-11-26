// Package mailbox
// @Title  多优先级队列管理器
// @Description  支持多个优先级队列，按调度策略（绝对优先/加权/公平）处理消息
// @Author  yr  2025/11/27
// @Update  yr  2025/11/27
package mailbox

import (
	"fmt"
	"sort"
	"sync"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/mpsc"
)

// PriorityQueueManager 多优先级队列管理器
// 职责：管理多个优先级队列，按照调度策略返回下一个待处理消息
// 调度策略：由 PriorityScheduler 决定（绝对优先/加权/公平）
type PriorityQueueManager struct {
	queues           map[def.Priority]queue[inf.IEvent] // 各优先级队列
	scheduler        *PriorityScheduler                 // 优先级调度器
	batchSizes       map[def.Priority]int               // 各优先级批量大小
	sortedPriorities []def.Priority                     // 预排序的优先级列表（数值小的优先级高）
	totalBatchLimit  int                                // 单次处理的总批次限制

	// 内存复用池
	availablePrioritiesPool sync.Pool
}

// NewPriorityQueueManager 创建多优先级队列管理器
func NewPriorityQueueManager(conf *config.MultiLevelQueueConf) *PriorityQueueManager {
	// 使用默认配置
	if conf == nil {
		conf = DefaultMultiLevelQueueConf()
	}

	m := &PriorityQueueManager{
		queues:           make(map[def.Priority]queue[inf.IEvent]),
		batchSizes:       make(map[def.Priority]int),
		sortedPriorities: make([]def.Priority, 0, len(conf.PriorityBatches)),
	}

	// 初始化内存池
	m.availablePrioritiesPool.New = func() interface{} {
		return make([]def.Priority, 0, 16)
	}

	// 初始化调度器
	m.scheduler = NewPriorityScheduler(&config.MultiLevelWorkerConf{
		Strategy:        conf.Strategy,
		PriorityBatches: conf.PriorityBatches,
	})

	// 初始化各优先级队列
	totalBatchSize := 0
	for priority, pc := range conf.PriorityBatches {
		m.queues[priority] = mpsc.New[inf.IEvent]()
		m.batchSizes[priority] = pc.BatchSize
		m.sortedPriorities = append(m.sortedPriorities, priority)
		totalBatchSize += pc.BatchSize
	}

	// 按优先级排序（数值越小优先级越高）
	sort.Slice(m.sortedPriorities, func(i, j int) bool {
		return m.sortedPriorities[i] < m.sortedPriorities[j]
	})

	// 设置总批次限制
	if conf.TotalBatchLimit > 0 {
		m.totalBatchLimit = conf.TotalBatchLimit
	} else if totalBatchSize > 0 {
		const maxReasonableBatch = 128
		if totalBatchSize > maxReasonableBatch {
			m.totalBatchLimit = maxReasonableBatch
		} else {
			m.totalBatchLimit = totalBatchSize
		}
	} else {
		m.totalBatchLimit = 32 // 默认值
	}

	return m
}

// Submit 提交事件到对应优先级队列
func (m *PriorityQueueManager) Submit(e inf.IEvent) error {
	priority := e.GetPriority()
	que, exists := m.queues[priority]
	if !exists {
		return fmt.Errorf("invalid priority: %d", priority)
	}

	que.Push(e)
	return nil
}

// NextEvent 获取下一个待处理事件
// 调度策略：根据调度器策略选择优先级，然后从对应队列弹出消息
func (m *PriorityQueueManager) NextEvent() (inf.IEvent, bool) {
	// 从对象池获取可复用切片
	availableSlice := m.availablePrioritiesPool.Get().([]def.Priority)
	available := availableSlice[:0]
	defer func() {
		m.availablePrioritiesPool.Put(available[:0])
	}()

	// 收集非空队列的优先级
	for _, priority := range m.sortedPriorities {
		if !m.queues[priority].Empty() {
			available = append(available, priority)
		}
	}

	// 没有可用消息
	if len(available) == 0 {
		return nil, false
	}

	// 使用调度器选择优先级
	selectedPriority := m.scheduler.NextPriorityWithOrdering(available)
	if selectedPriority == -1 {
		return nil, false
	}

	// 从选中的队列弹出消息
	if que, exists := m.queues[selectedPriority]; exists {
		return que.Pop()
	}

	return nil, false
}

// GetMsgLen 获取所有队列的总消息数量
func (m *PriorityQueueManager) GetMsgLen() int {
	total := 0
	for _, que := range m.queues {
		total += que.Len()
	}
	return total
}

// DrainAll 清空所有队列
func (m *PriorityQueueManager) DrainAll(handler func(inf.IEvent)) {
	// 按优先级从高到低处理剩余消息
	for _, priority := range m.sortedPriorities {
		que := m.queues[priority]
		for !que.Empty() {
			if e, ok := que.Pop(); ok {
				handler(e)
			}
		}
	}
}

// IsEmpty 判断所有队列是否为空
func (m *PriorityQueueManager) IsEmpty() bool {
	for _, que := range m.queues {
		if !que.Empty() {
			return false
		}
	}
	return true
}

// GetPriorityQueueLen 获取指定优先级队列的长度（扩展方法）
func (m *PriorityQueueManager) GetPriorityQueueLen(priority def.Priority) int {
	if que, exists := m.queues[priority]; exists {
		return que.Len()
	}
	return 0
}

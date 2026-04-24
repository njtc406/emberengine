// Package mailbox
// @Title  多优先级队列管理器
// @Description  支持多个优先级队列，按调度策略（绝对优先/加权/公平）处理消息
// @Author  yr  2025/11/27
// @Update  yr  2025/11/27
package mailbox

import (
	"fmt"
	"log/slog"
	"sort"
	"sync/atomic"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/mpsc"
)

// PriorityQueueManager 多优先级队列管理器
// 职责：管理多个优先级队列，按照调度策略返回下一个待处理消息
// 调度策略：由 PriorityScheduler 决定（绝对优先/加权/公平）
//
// 并发模型：单 Worker 独占（与 PriorityScheduler 一致），nextJobBuf 复用安全。
type PriorityQueueManager struct {
	queues           map[def.Priority]queue[inf.IMailboxJob] // 各优先级队列
	scheduler        *PriorityScheduler                      // 优先级调度器
	batchSizes       map[def.Priority]int                    // 各优先级批量大小
	sortedPriorities []def.Priority                          // 预排序的优先级列表（数值小的优先级高）
	nextJobBuf       []def.Priority                          // NextJob 复用 buffer，长度 = len(sortedPriorities)
	totalBatchLimit  int                                     // 单次处理的总批次限制
	fallbackPriority def.Priority                            // 未注册优先级的 fallback 目标
	fallbackWarned   atomic.Bool                             // fallback 日志 warn-once 标记
}

// NewPriorityQueueManager 创建多优先级队列管理器
func NewPriorityQueueManager(conf *config.MultiLevelQueueConf) *PriorityQueueManager {
	// 使用默认配置
	if conf == nil {
		conf = DefaultMultiLevelQueueConf()
	}

	m := &PriorityQueueManager{
		queues:           make(map[def.Priority]queue[inf.IMailboxJob]),
		batchSizes:       make(map[def.Priority]int),
		sortedPriorities: make([]def.Priority, 0, len(conf.PriorityBatches)),
	}

	// 确保配置至少包含一个优先级队列，避免空配置导致运行时丢消息
	if len(conf.PriorityBatches) == 0 {
		conf.PriorityBatches = map[def.Priority]*config.PriorityConfig{
			def.PriorityNormal: {BatchSize: 8},
		}
	}

	// 初始化调度器
	m.scheduler = NewPriorityScheduler(&config.MultiLevelWorkerConf{
		Strategy:        conf.Strategy,
		PriorityBatches: conf.PriorityBatches,
	})

	// 初始化各优先级队列
	totalBatchSize := 0
	for priority, pc := range conf.PriorityBatches {
		m.queues[priority] = mpsc.New[inf.IMailboxJob]()
		m.batchSizes[priority] = pc.BatchSize
		m.sortedPriorities = append(m.sortedPriorities, priority)
		totalBatchSize += pc.BatchSize
	}

	// 按优先级排序（数值越小优先级越高）
	sort.Slice(m.sortedPriorities, func(i, j int) bool {
		return m.sortedPriorities[i] < m.sortedPriorities[j]
	})

	// 预分配 NextJob 复用 buffer，长度等于注册的优先级数（避免 [16] 固定数组越界）
	m.nextJobBuf = make([]def.Priority, len(m.sortedPriorities))

	// 记录最低优先级，作为未注册优先级的 fallback 目标
	m.fallbackPriority = m.sortedPriorities[len(m.sortedPriorities)-1]

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
// 未注册的优先级会 fallback 到已注册的最低优先级队列
func (m *PriorityQueueManager) Submit(e inf.IMailboxJob) error {
	priority := e.GetPriority()
	que, exists := m.queues[priority]
	if !exists {
		// fallback 到最低优先级队列，避免静默丢弃消息
		que, exists = m.queues[m.fallbackPriority]
		if !exists {
			return fmt.Errorf("no available queue for priority: %d", priority)
		}
		// warn-once: 提示未注册的优先级发生了 fallback
		if m.fallbackWarned.CompareAndSwap(false, true) {
			slog.Warn("PriorityQueueManager: unregistered priority fallback",
				"requested", int(priority), "fallback", int(m.fallbackPriority))
		}
	}

	que.Push(e)
	return nil
}

// NextJob 获取下一个待处理事件
// 调度策略：根据调度器策略选择优先级，然后从对应队列弹出消息
func (m *PriorityQueueManager) NextJob() (inf.IMailboxJob, bool) {
	// 复用预分配的 buffer，长度严格匹配 sortedPriorities，避免越界 panic
	buf := m.nextJobBuf
	n := 0

	// 收集非空队列的优先级
	for _, priority := range m.sortedPriorities {
		if !m.queues[priority].Empty() {
			buf[n] = priority
			n++
		}
	}

	// 没有可用消息
	if n == 0 {
		return nil, false
	}

	// 使用调度器选择优先级
	selectedPriority := m.scheduler.NextPriorityWithOrdering(buf[:n])
	if selectedPriority == -1 {
		return nil, false
	}

	// 从选中的队列弹出消息
	if que, exists := m.queues[selectedPriority]; exists {
		return que.Pop()
	}

	return nil, false
}

// GetJobLen 获取所有队列的总消息数量
func (m *PriorityQueueManager) GetJobLen() int {
	total := 0
	for _, que := range m.queues {
		total += que.Len()
	}
	return total
}

// DrainAll 清空所有队列
func (m *PriorityQueueManager) DrainAll(handler func(inf.IMailboxJob)) {
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

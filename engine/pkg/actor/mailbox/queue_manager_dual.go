// Package mailbox
// @Title  双队列管理器
// @Description  实现系统队列（高优先级）+ 用户队列（普通优先级）的双队列模式
// @Author  yr  2025/11/27
// @Update  yr  2025/11/27
package mailbox

import (
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/mpsc"
)

// DualQueueManager 双队列管理器
// 职责：将消息分为系统消息（高优先级）和用户消息（普通优先级）两个队列
// 调度策略：始终优先处理系统消息
type DualQueueManager struct {
	systemMailbox queue[inf.IEvent] // 系统队列：PrioritySys/Urgent/High
	userMailbox   queue[inf.IEvent] // 用户队列：PriorityNormal/Low/Batch
}

// NewDualQueueManager 创建双队列管理器
func NewDualQueueManager() *DualQueueManager {
	return &DualQueueManager{
		systemMailbox: mpsc.New[inf.IEvent](),
		userMailbox:   mpsc.New[inf.IEvent](),
	}
}

// Submit 提交事件到对应队列
func (m *DualQueueManager) Submit(e inf.IEvent) error {
	// 按优先级分配队列：< Normal 的进系统队列，>= Normal 的进用户队列
	if e.GetPriority() < def.PriorityNormal {
		m.systemMailbox.Push(e)
	} else {
		m.userMailbox.Push(e)
	}
	return nil
}

// NextEvent 获取下一个待处理事件
// 调度策略：优先返回系统队列的消息，系统队列为空时返回用户队列消息
func (m *DualQueueManager) NextEvent() (inf.IEvent, bool) {
	// 优先处理系统消息
	if e, ok := m.systemMailbox.Pop(); ok {
		return e, true
	}

	// 再处理用户消息
	if e, ok := m.userMailbox.Pop(); ok {
		return e, true
	}

	return nil, false
}

// GetMsgLen 获取所有队列的总消息数量
func (m *DualQueueManager) GetMsgLen() int {
	total := 0
	if m.systemMailbox != nil {
		total += m.systemMailbox.Len()
	}
	if m.userMailbox != nil {
		total += m.userMailbox.Len()
	}
	return total
}

// DrainAll 清空所有队列
func (m *DualQueueManager) DrainAll(handler func(inf.IEvent)) {
	// 先清空系统队列
	for !m.systemMailbox.Empty() {
		if e, ok := m.systemMailbox.Pop(); ok {
			handler(e)
		}
	}

	// 再清空用户队列
	for !m.userMailbox.Empty() {
		if e, ok := m.userMailbox.Pop(); ok {
			handler(e)
		}
	}
}

// IsEmpty 判断所有队列是否为空
func (m *DualQueueManager) IsEmpty() bool {
	return m.systemMailbox.Empty() && m.userMailbox.Empty()
}

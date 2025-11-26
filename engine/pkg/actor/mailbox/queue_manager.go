// Package mailbox
// @Title  队列管理器
// @Description  抽象队列管理接口，支持双队列和多优先级队列两种实现
// @Author  yr  2025/11/27
// @Update  yr  2025/11/27
package mailbox

import (
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

// IQueueManager 队列管理器接口
// 职责：管理消息队列的提交、获取、统计等操作
// 实现：支持双队列（DualQueueManager）和多优先级队列（PriorityQueueManager）
type IQueueManager interface {
	// Submit 提交事件到队列
	Submit(e inf.IEvent) error

	// NextEvent 获取下一个待处理事件（按优先级或策略）
	// 返回：事件对象，是否成功获取
	NextEvent() (inf.IEvent, bool)

	// GetMsgLen 获取所有队列的总消息数量
	GetMsgLen() int

	// DrainAll 清空所有队列，对每个事件执行处理函数
	// 用于 Worker 关闭时处理剩余消息
	DrainAll(handler func(inf.IEvent))

	// IsEmpty 判断所有队列是否为空
	IsEmpty() bool
}

// queue 内部队列接口（与 WorkerPool 中的定义保持一致）
type queue[T any] interface {
	Push(T) bool
	Pop() (T, bool)
	BatchPop(int) []T
	Empty() bool
	Len() int
}

package event

import (
	"sync/atomic"

	"github.com/njtc406/emberengine/engine/pkg/def"
)

// processBatchedEvents 处理批量事件
func (eb *Bus) processBatchedEvents() {
	for {
		select {
		case <-eb.batchTicker.C:
			eb.flushAllBuffers()
		case <-eb.batchStop:
			return
		}
	}
}

// flushAllBuffers 刷新所有缓冲区
func (eb *Bus) flushAllBuffers() {
	// 注意：不要在持有 bufferMutex 时执行 publish/post job，避免阻塞批处理和生产者。
	eb.bufferMutex.Lock()
	if len(eb.eventBuffer) == 0 {
		eb.bufferMutex.Unlock()
		return
	}
	batches := make(map[def.EventType][]*busEvent, len(eb.eventBuffer))
	for eventType, events := range eb.eventBuffer {
		if len(events) > 0 {
			batches[eventType] = events
		}
	}
	// 直接替换 map，避免遍历时 delete
	eb.eventBuffer = make(map[def.EventType][]*busEvent)
	eb.bufferMutex.Unlock()

	for eventType, events := range batches {
		eb.flushEventBatch(eventType, events)
	}
}

// flushEventBatch 刷新指定类型的事件批量
func (eb *Bus) flushEventBatch(eventType def.EventType, events []*busEvent) {
	if len(events) == 0 {
		return
	}

	classification := eb.eventRegistry.GetClassification(eventType)

	// 按照事件范围选择批量处理策略
	switch classification.Scope {
	case ScopeGlobal:
		for _, be := range events {
			eb.publishGlobal(be.ctx, be.event)
		}
	case ScopeCluster, ScopeRegion, ScopeNode:
		for _, be := range events {
			eb.publishServer(be.ctx, be.event)
		}
	default:
		// 本地事件直接处理
		for _, be := range events {
			eb.publishGlobal(be.ctx, be.event) // 默认作为全局事件处理
		}
	}

	// 更新指标
	atomic.AddInt64(&eb.metrics.TotalBatched, int64(len(events)))
	atomic.AddInt64(&eb.metrics.TotalDelivered, int64(len(events)))
}

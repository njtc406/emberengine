package msgbus

import (
	"sync/atomic"
)

// RpcMetrics 提供 MessageBus 级别的 RPC 聚合指标。
// 所有字段使用 atomic 操作，安全用于并发读写。
// 由 MessageBusFactory 持有，通过 GetRpcMetrics() 返回快照。
type RpcMetrics struct {
	// Call 计数
	CallTotal    int64 // Call + CallWithOpt 总调用次数
	CallErrors   int64 // Call 失败次数
	CallInFlight int64 // Call 当前进行中（同步调用阻塞期间）

	// AsyncCall 计数
	AsyncCallTotal  int64 // AsyncCall + AsyncCallWithOpt 总调用次数
	AsyncCallErrors int64 // AsyncCall 失败次数

	// Send 计数
	SendTotal  int64 // Send + SendWithOpt 总调用次数
	SendErrors int64 // Send 失败次数
}

// rpcMetricsCollector 内部 atomic 计数器，嵌入 MessageBusFactory。
type rpcMetricsCollector struct {
	callTotal    atomic.Int64
	callErrors   atomic.Int64
	callInFlight atomic.Int64

	asyncCallTotal  atomic.Int64
	asyncCallErrors atomic.Int64

	sendTotal  atomic.Int64
	sendErrors atomic.Int64
}

// snapshot 返回当前计数器的快照副本。
func (c *rpcMetricsCollector) snapshot() RpcMetrics {
	return RpcMetrics{
		CallTotal:       c.callTotal.Load(),
		CallErrors:      c.callErrors.Load(),
		CallInFlight:    c.callInFlight.Load(),
		AsyncCallTotal:  c.asyncCallTotal.Load(),
		AsyncCallErrors: c.asyncCallErrors.Load(),
		SendTotal:       c.sendTotal.Load(),
		SendErrors:      c.sendErrors.Load(),
	}
}

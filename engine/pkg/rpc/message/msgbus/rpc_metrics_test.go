package msgbus

import (
	"testing"
)

func TestRpcMetricsCollector_Snapshot(t *testing.T) {
	var c rpcMetricsCollector

	// 初始状态全零
	m := c.snapshot()
	if m.CallTotal != 0 || m.CallErrors != 0 || m.CallInFlight != 0 {
		t.Errorf("initial call metrics should be zero: %+v", m)
	}
	if m.AsyncCallTotal != 0 || m.AsyncCallErrors != 0 {
		t.Errorf("initial async metrics should be zero: %+v", m)
	}
	if m.SendTotal != 0 || m.SendErrors != 0 {
		t.Errorf("initial send metrics should be zero: %+v", m)
	}

	// 递增
	c.callTotal.Add(10)
	c.callErrors.Add(2)
	c.callInFlight.Add(3)
	c.asyncCallTotal.Add(20)
	c.asyncCallErrors.Add(1)
	c.sendTotal.Add(50)
	c.sendErrors.Add(5)

	m = c.snapshot()
	if m.CallTotal != 10 {
		t.Errorf("CallTotal = %d, want 10", m.CallTotal)
	}
	if m.CallErrors != 2 {
		t.Errorf("CallErrors = %d, want 2", m.CallErrors)
	}
	if m.CallInFlight != 3 {
		t.Errorf("CallInFlight = %d, want 3", m.CallInFlight)
	}
	if m.AsyncCallTotal != 20 {
		t.Errorf("AsyncCallTotal = %d, want 20", m.AsyncCallTotal)
	}
	if m.AsyncCallErrors != 1 {
		t.Errorf("AsyncCallErrors = %d, want 1", m.AsyncCallErrors)
	}
	if m.SendTotal != 50 {
		t.Errorf("SendTotal = %d, want 50", m.SendTotal)
	}
	if m.SendErrors != 5 {
		t.Errorf("SendErrors = %d, want 5", m.SendErrors)
	}
}

func TestRpcMetricsCollector_InFlightDecrement(t *testing.T) {
	var c rpcMetricsCollector

	c.callInFlight.Add(5)
	c.callInFlight.Add(-3)

	m := c.snapshot()
	if m.CallInFlight != 2 {
		t.Errorf("CallInFlight = %d, want 2", m.CallInFlight)
	}
}

func TestMessageBusFactory_GetRpcMetrics(t *testing.T) {
	f := &MessageBusFactory{}

	// 模拟一些指标
	f.metrics.callTotal.Add(100)
	f.metrics.sendErrors.Add(3)

	m := f.GetRpcMetrics()
	if m.CallTotal != 100 {
		t.Errorf("CallTotal = %d, want 100", m.CallTotal)
	}
	if m.SendErrors != 3 {
		t.Errorf("SendErrors = %d, want 3", m.SendErrors)
	}
}

func TestRpcMetrics_ConcurrentSnapshot(t *testing.T) {
	var c rpcMetricsCollector
	done := make(chan struct{})

	// 并发写入
	go func() {
		for i := 0; i < 10000; i++ {
			c.callTotal.Add(1)
			c.callErrors.Add(1)
			c.callInFlight.Add(1)
			c.callInFlight.Add(-1)
			c.asyncCallTotal.Add(1)
			c.sendTotal.Add(1)
		}
		close(done)
	}()

	// 并发读取
	for i := 0; i < 100; i++ {
		m := c.snapshot()
		// 所有值应 >= 0
		if m.CallTotal < 0 || m.CallErrors < 0 || m.CallInFlight < 0 {
			t.Errorf("negative call metrics: %+v", m)
		}
	}

	<-done
	m := c.snapshot()
	if m.CallTotal != 10000 {
		t.Errorf("final CallTotal = %d, want 10000", m.CallTotal)
	}
}

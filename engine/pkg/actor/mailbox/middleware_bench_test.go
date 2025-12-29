package mailbox

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

// mockEvent 用于测试的模拟事件
type mockEvent struct {
	priority      def.Priority
	dispatcherKey string
}

func (e *mockEvent) GetPriority() def.Priority { return e.priority }
func (e *mockEvent) GetDispatcherKey() string  { return e.dispatcherKey }
func (e *mockEvent) GetType() int32            { return 0 }
func (e *mockEvent) GetEventType() int32       { return 0 }
func (e *mockEvent) Release()                  {}
func (e *mockEvent) IsRef() bool               { return false }
func (e *mockEvent) Ref()                      {}
func (e *mockEvent) UnRef() bool               { return false }
func (e *mockEvent) GetCallID() uint64         { return 0 }
func (e *mockEvent) SetCallID(uint64)          {}
func (e *mockEvent) IsRPCReply() bool          { return false }
func (e *mockEvent) IsCallback() bool          { return false }
func (e *mockEvent) GetData() interface{}      { return nil }
func (e *mockEvent) SetData(interface{})       {}
func (e *mockEvent) GetServiceName() string    { return "" }
func (e *mockEvent) SetServiceName(string)     {}
func (e *mockEvent) GetMethodName() string     { return "" }
func (e *mockEvent) SetMethodName(string)      {}
func (e *mockEvent) GetArgs() []interface{}    { return nil }
func (e *mockEvent) SetArgs([]interface{})     {}
func (e *mockEvent) GetReply() interface{}     { return nil }
func (e *mockEvent) SetReply(interface{})      {}
func (e *mockEvent) GetError() error           { return nil }
func (e *mockEvent) SetError(error)            {}
func (e *mockEvent) GetTimeout() time.Duration { return 0 }
func (e *mockEvent) SetTimeout(time.Duration)  {}
func (e *mockEvent) GetDeadline() time.Time    { return time.Time{} }
func (e *mockEvent) SetDeadline(time.Time)     {}
func (e *mockEvent) IsExpired() bool           { return false }
func (e *mockEvent) GetSource() string         { return "" }
func (e *mockEvent) SetSource(string)          {}
func (e *mockEvent) GetTarget() string         { return "" }
func (e *mockEvent) SetTarget(string)          {}

// ============================================================================
// 基准测试：单个中间件性能
// ============================================================================

// BenchmarkDispatchKeyStatsMiddleware_OnReceive 测试 DispatchKey 统计中间件
func BenchmarkDispatchKeyStatsMiddleware_OnReceive(b *testing.B) {
	m := NewDispatchKeyStatsMiddleware(nil, 10*time.Second, 10)
	evt := &mockEvent{dispatcherKey: "test-key"}
	mctx := NewMiddlewareContext(context.Background(), evt, "test-service")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			m.OnReceive(mctx)
		}
	})
	b.ReportAllocs()
}

// BenchmarkDispatchKeyStatsMiddleware_HighCardinality 测试高基数场景
func BenchmarkDispatchKeyStatsMiddleware_HighCardinality(b *testing.B) {
	m := NewDispatchKeyStatsMiddleware(nil, 10*time.Second, 10)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			evt := &mockEvent{dispatcherKey: "key-" + itoa(i%1000)} // 1000个不同的key
			mctx := NewMiddlewareContext(context.Background(), evt, "test-service")
			m.OnReceive(mctx)
			i++
		}
	})
	b.ReportAllocs()
}

// BenchmarkRateLimitMiddleware_OnReceive 测试限流中间件
func BenchmarkRateLimitMiddleware_OnReceive(b *testing.B) {
	m := NewRateLimitMiddleware(100000, 10000) // 10w QPS, 1w burst
	evt := &mockEvent{}
	mctx := NewMiddlewareContext(context.Background(), evt, "test-service")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			m.OnReceive(mctx)
		}
	})
	b.ReportAllocs()
}

// BenchmarkRateLimitMiddleware_WithSkip 测试限流中间件跳过逻辑
func BenchmarkRateLimitMiddleware_WithSkip(b *testing.B) {
	m := NewRateLimitMiddleware(
		100000, 10000,
		WithRateLimitSkipFunc(func(mctx inf.IMiddlewareContext) bool {
			return mctx.Event().GetPriority() <= def.PriorityUrgent
		}),
	)
	evt := &mockEvent{priority: def.PriorityUrgent}
	mctx := NewMiddlewareContext(context.Background(), evt, "test-service")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			m.OnReceive(mctx)
		}
	})
	b.ReportAllocs()
}

// BenchmarkCircuitBreakerMiddleware_Closed 测试熔断器关闭状态（快速路径）
func BenchmarkCircuitBreakerMiddleware_Closed(b *testing.B) {
	m := NewCircuitBreakerMiddleware(5, 3)
	evt := &mockEvent{}
	mctx := NewMiddlewareContext(context.Background(), evt, "test-service")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			m.OnReceive(mctx)
		}
	})
	b.ReportAllocs()
}

// BenchmarkCircuitBreakerMiddleware_Open 测试熔断器打开状态
func BenchmarkCircuitBreakerMiddleware_Open(b *testing.B) {
	m := NewCircuitBreakerMiddleware(5, 3)
	m.state.Store(int32(StateOpen))
	m.lastFailTime.Store(time.Now().UnixNano())

	evt := &mockEvent{}
	mctx := NewMiddlewareContext(context.Background(), evt, "test-service")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			m.OnReceive(mctx)
		}
	})
	b.ReportAllocs()
}

// BenchmarkCircuitBreakerMiddleware_HalfOpen 测试熔断器半开状态
func BenchmarkCircuitBreakerMiddleware_HalfOpen(b *testing.B) {
	m := NewCircuitBreakerMiddleware(5, 3)
	m.state.Store(int32(StateHalfOpen))
	m.halfOpenReqs.Store(0)

	evt := &mockEvent{}
	mctx := NewMiddlewareContext(context.Background(), evt, "test-service")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			m.OnReceive(mctx)
		}
	})
	b.ReportAllocs()
}

// BenchmarkCircuitBreakerMiddleware_StateTransition 测试状态转换性能
func BenchmarkCircuitBreakerMiddleware_StateTransition(b *testing.B) {
	evt := &mockEvent{}
	mctx := NewMiddlewareContext(context.Background(), evt, "test-service")

	b.Run("Closed->Open", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			m := NewCircuitBreakerMiddleware(5, 3)
			// 触发5次失败
			for j := 0; j < 5; j++ {
				m.OnReceive(mctx)
				m.OnComplete(mctx, errors.New("test error"), nil)
			}
		}
	})

	b.Run("Open->HalfOpen", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			m := NewCircuitBreakerMiddleware(5, 3, WithCooldownDuration(1*time.Nanosecond))
			m.state.Store(int32(StateOpen))
			m.lastFailTime.Store(time.Now().Add(-1 * time.Second).UnixNano())
			m.OnReceive(mctx)
		}
	})

	b.Run("HalfOpen->Closed", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			m := NewCircuitBreakerMiddleware(5, 3)
			m.state.Store(int32(StateHalfOpen))
			m.halfOpenReqs.Store(1)
			// 触发3次成功
			for j := 0; j < 3; j++ {
				m.OnComplete(mctx, nil, nil)
			}
		}
	})
}

// ============================================================================
// 基准测试：中间件链性能
// ============================================================================

// BenchmarkMiddlewareChain_Empty 测试空中间件链
func BenchmarkMiddlewareChain_Empty(b *testing.B) {
	chain := NewMiddlewareChain()
	evt := &mockEvent{}
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			chain.ExecuteOnReceive(ctx, evt, "test-service")
		}
	})
	b.ReportAllocs()
}

// BenchmarkMiddlewareChain_Single 测试单个中间件
func BenchmarkMiddlewareChain_Single(b *testing.B) {
	m := NewDispatchKeyStatsMiddleware(nil, 10*time.Second, 10)
	chain := NewMiddlewareChain(m)
	evt := &mockEvent{dispatcherKey: "test-key"}
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			chain.ExecuteOnReceive(ctx, evt, "test-service")
		}
	})
	b.ReportAllocs()
}

// BenchmarkMiddlewareChain_Multiple 测试多个中间件
func BenchmarkMiddlewareChain_Multiple(b *testing.B) {
	stats := NewDispatchKeyStatsMiddleware(nil, 10*time.Second, 10)
	rateLimit := NewRateLimitMiddleware(100000, 10000)
	breaker := NewCircuitBreakerMiddleware(5, 3)

	chain := NewMiddlewareChain(stats, rateLimit, breaker)
	evt := &mockEvent{dispatcherKey: "test-key"}
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			chain.ExecuteOnReceive(ctx, evt, "test-service")
		}
	})
	b.ReportAllocs()
}

// BenchmarkMiddlewareChain_OnComplete 测试 OnComplete 性能
func BenchmarkMiddlewareChain_OnComplete(b *testing.B) {
	stats := NewDispatchKeyStatsMiddleware(nil, 10*time.Second, 10)
	rateLimit := NewRateLimitMiddleware(100000, 10000)
	breaker := NewCircuitBreakerMiddleware(5, 3)

	chain := NewMiddlewareChain(stats, rateLimit, breaker)
	evt := &mockEvent{dispatcherKey: "test-key"}
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, mctx := chain.ExecuteOnReceive(ctx, evt, "test-service")
			chain.ExecuteOnComplete(mctx, nil, nil)
		}
	})
	b.ReportAllocs()
}

// ============================================================================
// 基准测试：并发竞争场景
// ============================================================================

// BenchmarkCircuitBreakerMiddleware_ConcurrentStateChange 测试并发状态转换
func BenchmarkCircuitBreakerMiddleware_ConcurrentStateChange(b *testing.B) {
	m := NewCircuitBreakerMiddleware(5, 3, WithCooldownDuration(100*time.Microsecond))
	evt := &mockEvent{}
	mctx := NewMiddlewareContext(context.Background(), evt, "test-service")

	var successCount atomic.Uint64
	var failCount atomic.Uint64

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			result := m.OnReceive(mctx)
			if result.Action == inf.ActionContinue {
				successCount.Add(1)
				// 模拟50%失败率
				if successCount.Load()%2 == 0 {
					m.OnComplete(mctx, errors.New("test error"), nil)
					failCount.Add(1)
				} else {
					m.OnComplete(mctx, nil, nil)
				}
			}
		}
	})

	b.Logf("Success: %d, Failed: %d, State: %s",
		successCount.Load(), failCount.Load(), m.GetState().String())
}

// BenchmarkRateLimitMiddleware_Contention 测试限流竞争场景
func BenchmarkRateLimitMiddleware_Contention(b *testing.B) {
	m := NewRateLimitMiddleware(10000, 1000) // 降低速率以触发限流
	evt := &mockEvent{}
	mctx := NewMiddlewareContext(context.Background(), evt, "test-service")

	var accepted atomic.Uint64
	var rejected atomic.Uint64

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			result := m.OnReceive(mctx)
			if result.Action == inf.ActionContinue {
				accepted.Add(1)
			} else {
				rejected.Add(1)
			}
		}
	})

	b.Logf("Accepted: %d, Rejected: %d, Rate: %.2f%%",
		accepted.Load(), rejected.Load(),
		float64(accepted.Load())*100/float64(accepted.Load()+rejected.Load()))
}

// ============================================================================
// 基准测试：内存分配
// ============================================================================

// BenchmarkMiddlewareContext_Creation 测试中间件上下文创建
func BenchmarkMiddlewareContext_Creation(b *testing.B) {
	evt := &mockEvent{}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NewMiddlewareContext(ctx, evt, "test-service")
	}
	b.ReportAllocs()
}

// BenchmarkMiddlewareContext_SetGet 测试上下文数据存取
func BenchmarkMiddlewareContext_SetGet(b *testing.B) {
	evt := &mockEvent{}
	mctx := NewMiddlewareContext(context.Background(), evt, "test-service")

	b.Run("Set", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				mctx.Set("key"+itoa(i%10), i)
				i++
			}
		})
		b.ReportAllocs()
	})

	b.Run("Get", func(b *testing.B) {
		mctx.Set("key1", 123)
		mctx.Set("key2", "value")
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				mctx.Get("key" + itoa(i%2+1))
				i++
			}
		})
		b.ReportAllocs()
	})
}

// ============================================================================
// 基准测试：真实场景模拟
// ============================================================================

// BenchmarkRealisticWorkload 模拟真实工作负载
func BenchmarkRealisticWorkload(b *testing.B) {
	// 创建完整的中间件链
	stats := NewDispatchKeyStatsMiddleware(nil, 10*time.Second, 10)
	rateLimit := NewRateLimitMiddleware(100000, 10000)
	breaker := NewCircuitBreakerMiddleware(10, 5)
	chain := NewMiddlewareChain(stats, rateLimit, breaker)

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			// 模拟不同的 dispatcherKey（1000个不同的key）
			evt := &mockEvent{
				dispatcherKey: "user-" + itoa(i%1000),
				priority:      def.PriorityNormal,
			}

			// 执行中间件链
			result, mctx := chain.ExecuteOnReceive(ctx, evt, "test-service")
			if result.Action == inf.ActionContinue {
				// 模拟5%的失败率
				var err error
				if i%20 == 0 {
					err = errors.New("simulated error")
				}
				chain.ExecuteOnComplete(mctx, err, nil)
			}
			i++
		}
	})
	b.ReportAllocs()
}

// BenchmarkHighThroughputScenario 高吞吐场景
func BenchmarkHighThroughputScenario(b *testing.B) {
	// 只使用 DispatchKey 统计，模拟高吞吐场景
	stats := NewDispatchKeyStatsMiddleware(nil, 10*time.Second, 10)
	chain := NewMiddlewareChain(stats)

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			evt := &mockEvent{dispatcherKey: "key-" + itoa(i%100)}
			result, mctx := chain.ExecuteOnReceive(ctx, evt, "test-service")
			if result.Action == inf.ActionContinue {
				chain.ExecuteOnComplete(mctx, nil, nil)
			}
			i++
		}
	})
	b.ReportAllocs()
}

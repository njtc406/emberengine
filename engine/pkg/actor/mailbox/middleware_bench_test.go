package mailbox

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	mbjob "github.com/njtc406/emberengine/engine/pkg/actor/mailbox/job"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

func newBenchJob(ctx context.Context, priority def.Priority, dispatcherKey string) inf.IMailboxJob {
	j := mbjob.NewEventBusJob()
	j.SetContext(ctx)
	j.SetPriority(priority)
	j.SetDispatcherKey(dispatcherKey)
	return j
}

// BenchmarkDispatchKeyStatsMiddleware_OnReceive 测试 DispatchKey 统计中间件
func BenchmarkDispatchKeyStatsMiddleware_OnReceive(b *testing.B) {
	m := NewDispatchKeyStatsMiddleware(nil, 10*time.Second, 10, 0)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		job := newBenchJob(context.Background(), def.PriorityNormal, "test-key")
		defer job.Release()
		mctx := NewMiddlewareContext(job.GetContext(), job, "test-service")
		for pb.Next() {
			m.OnReceive(mctx)
		}
	})
	b.ReportAllocs()
}

// BenchmarkDispatchKeyStatsMiddleware_HighCardinality 高基数场景
func BenchmarkDispatchKeyStatsMiddleware_HighCardinality(b *testing.B) {
	m := NewDispatchKeyStatsMiddleware(nil, 10*time.Second, 10, 0)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		job := newBenchJob(context.Background(), def.PriorityNormal, "")
		defer job.Release()
		mctx := NewMiddlewareContext(job.GetContext(), job, "test-service")
		i := 0
		for pb.Next() {
			job.SetDispatcherKey("key-" + itoa(i%1000))
			m.OnReceive(mctx)
			i++
		}
	})
	b.ReportAllocs()
}

// BenchmarkRateLimitMiddleware_OnReceive 测试限流中间件
func BenchmarkRateLimitMiddleware_OnReceive(b *testing.B) {
	m := NewRateLimitMiddleware(100000, 10000)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		job := newBenchJob(context.Background(), def.PriorityNormal, "")
		defer job.Release()
		mctx := NewMiddlewareContext(job.GetContext(), job, "test-service")
		for pb.Next() {
			m.OnReceive(mctx)
		}
	})
	b.ReportAllocs()
}

// BenchmarkRateLimitMiddleware_WithSkip 限流跳过逻辑
func BenchmarkRateLimitMiddleware_WithSkip(b *testing.B) {
	m := NewRateLimitMiddleware(
		100000, 10000,
		WithRateLimitSkipFunc(func(mctx inf.IMiddlewareContext) bool {
			return mctx.Job().GetPriority() <= def.PriorityUrgent
		}),
	)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		job := newBenchJob(context.Background(), def.PriorityUrgent, "")
		defer job.Release()
		mctx := NewMiddlewareContext(job.GetContext(), job, "test-service")
		for pb.Next() {
			m.OnReceive(mctx)
		}
	})
	b.ReportAllocs()
}

// BenchmarkMiddlewareChain_Empty 空中间件链
func BenchmarkMiddlewareChain_Empty(b *testing.B) {
	chain := NewMiddlewareChain(nil)
	ctx := context.Background()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		job := newBenchJob(ctx, def.PriorityNormal, "")
		defer job.Release()
		for pb.Next() {
			chain.ExecuteOnReceive(job, "test-service")
		}
	})
	b.ReportAllocs()
}

// BenchmarkMiddlewareChain_Single 单个中间件
func BenchmarkMiddlewareChain_Single(b *testing.B) {
	m := NewDispatchKeyStatsMiddleware(nil, 10*time.Second, 10, 0)
	chain := NewMiddlewareChain([]inf.IMailboxMiddleware{m})
	ctx := context.Background()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		job := newBenchJob(ctx, def.PriorityNormal, "test-key")
		defer job.Release()
		for pb.Next() {
			chain.ExecuteOnReceive(job, "test-service")
		}
	})
	b.ReportAllocs()
}

// BenchmarkMiddlewareChain_Multiple 多个中间件
func BenchmarkMiddlewareChain_Multiple(b *testing.B) {
	stats := NewDispatchKeyStatsMiddleware(nil, 10*time.Second, 10, 0)
	rateLimit := NewRateLimitMiddleware(100000, 10000)
	chain := NewMiddlewareChain([]inf.IMailboxMiddleware{stats, rateLimit})
	ctx := context.Background()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		job := newBenchJob(ctx, def.PriorityNormal, "test-key")
		defer job.Release()
		for pb.Next() {
			chain.ExecuteOnReceive(job, "test-service")
		}
	})
	b.ReportAllocs()
}

// BenchmarkMiddlewareChain_OnComplete OnComplete 链路
func BenchmarkMiddlewareChain_OnComplete(b *testing.B) {
	stats := NewDispatchKeyStatsMiddleware(nil, 10*time.Second, 10, 0)
	rateLimit := NewRateLimitMiddleware(100000, 10000)
	chain := NewMiddlewareChain([]inf.IMailboxMiddleware{stats, rateLimit})
	ctx := context.Background()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		job := newBenchJob(ctx, def.PriorityNormal, "test-key")
		defer job.Release()
		for pb.Next() {
			_, mctx := chain.ExecuteOnReceive(job, "test-service")
			chain.ExecuteOnComplete(mctx, nil, nil)
		}
	})
	b.ReportAllocs()
}

// BenchmarkRateLimitMiddleware_Contention 限流竞争场景
func BenchmarkRateLimitMiddleware_Contention(b *testing.B) {
	m := NewRateLimitMiddleware(10000, 1000)
	var accepted atomic.Uint64
	var rejected atomic.Uint64
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		job := newBenchJob(context.Background(), def.PriorityNormal, "")
		defer job.Release()
		mctx := NewMiddlewareContext(job.GetContext(), job, "test-service")
		for pb.Next() {
			result := m.OnReceive(mctx)
			if result.Action == def.ActionContinue {
				accepted.Add(1)
			} else {
				rejected.Add(1)
			}
		}
	})
	total := accepted.Load() + rejected.Load()
	if total == 0 {
		total = 1
	}
	b.Logf("Accepted: %d, Rejected: %d, Rate: %.2f%%",
		accepted.Load(), rejected.Load(),
		float64(accepted.Load())*100/float64(total))
}

// BenchmarkMiddlewareContext_Creation 上下文创建
func BenchmarkMiddlewareContext_Creation(b *testing.B) {
	ctx := context.Background()
	job := newBenchJob(ctx, def.PriorityNormal, "")
	defer job.Release()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NewMiddlewareContext(ctx, job, "test-service")
	}
	b.ReportAllocs()
}

// BenchmarkMiddlewareContext_SetGet Set/Get
func BenchmarkMiddlewareContext_SetGet(b *testing.B) {
	job := newBenchJob(context.Background(), def.PriorityNormal, "")
	defer job.Release()
	mctx := NewMiddlewareContext(job.GetContext(), job, "test-service")

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

// BenchmarkRealisticWorkload 模拟真实负载
func BenchmarkRealisticWorkload(b *testing.B) {
	stats := NewDispatchKeyStatsMiddleware(nil, 10*time.Second, 10, 0)
	rateLimit := NewRateLimitMiddleware(100000, 10000)
	chain := NewMiddlewareChain([]inf.IMailboxMiddleware{stats, rateLimit})
	ctx := context.Background()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		job := newBenchJob(ctx, def.PriorityNormal, "")
		defer job.Release()
		i := 0
		for pb.Next() {
			job.SetDispatcherKey("user-" + itoa(i%1000))
			job.SetPriority(def.PriorityNormal)
			result, mctx := chain.ExecuteOnReceive(job, "test-service")
			if result.Action == def.ActionContinue {
				chain.ExecuteOnComplete(mctx, nil, nil)
			}
			i++
		}
	})
	b.ReportAllocs()
}

// BenchmarkHighThroughputScenario 高吞吐场景
func BenchmarkHighThroughputScenario(b *testing.B) {
	stats := NewDispatchKeyStatsMiddleware(nil, 10*time.Second, 10, 0)
	chain := NewMiddlewareChain([]inf.IMailboxMiddleware{stats})
	ctx := context.Background()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		job := newBenchJob(ctx, def.PriorityNormal, "")
		defer job.Release()
		i := 0
		for pb.Next() {
			job.SetDispatcherKey("key-" + itoa(i%100))
			result, mctx := chain.ExecuteOnReceive(job, "test-service")
			if result.Action == def.ActionContinue {
				chain.ExecuteOnComplete(mctx, nil, nil)
			}
			i++
		}
	})
	b.ReportAllocs()
}

package mailbox

import (
	"context"
	"errors"
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

// 鍩哄噯娴嬭瘯锛氬崟涓腑闂翠欢鎬ц兘
// ============================================================================

// BenchmarkDispatchKeyStatsMiddleware_OnReceive 娴嬭瘯 DispatchKey 缁熻涓棿浠?
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

// BenchmarkDispatchKeyStatsMiddleware_HighCardinality 娴嬭瘯楂樺熀鏁板満鏅?
func BenchmarkDispatchKeyStatsMiddleware_HighCardinality(b *testing.B) {
	m := NewDispatchKeyStatsMiddleware(nil, 10*time.Second, 10, 0)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		job := newBenchJob(context.Background(), def.PriorityNormal, "")
		defer job.Release()
		mctx := NewMiddlewareContext(job.GetContext(), job, "test-service")
		i := 0
		for pb.Next() {
			job.SetDispatcherKey("key-" + itoa(i%1000)) // 1000涓笉鍚岀殑key
			m.OnReceive(mctx)
			i++
		}
	})
	b.ReportAllocs()
}

// BenchmarkRateLimitMiddleware_OnReceive 娴嬭瘯闄愭祦涓棿浠?
func BenchmarkRateLimitMiddleware_OnReceive(b *testing.B) {
	m := NewRateLimitMiddleware(100000, 10000) // 10w QPS, 1w burst
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

// BenchmarkRateLimitMiddleware_WithSkip 娴嬭瘯闄愭祦涓棿浠惰烦杩囬€昏緫
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

// BenchmarkCircuitBreakerMiddleware_Closed 娴嬭瘯鐔旀柇鍣ㄥ叧闂姸鎬侊紙蹇€熻矾寰勶級
func BenchmarkCircuitBreakerMiddleware_Closed(b *testing.B) {
	m := NewCircuitBreakerMiddleware(5, 3)
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

// BenchmarkCircuitBreakerMiddleware_Open 娴嬭瘯鐔旀柇鍣ㄦ墦寮€鐘舵€?
func BenchmarkCircuitBreakerMiddleware_Open(b *testing.B) {
	m := NewCircuitBreakerMiddleware(5, 3)
	m.state.Store(int32(StateOpen))
	m.lastFailTime.Store(time.Now().UnixNano())
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

// BenchmarkCircuitBreakerMiddleware_HalfOpen 娴嬭瘯鐔旀柇鍣ㄥ崐寮€鐘舵€?
func BenchmarkCircuitBreakerMiddleware_HalfOpen(b *testing.B) {
	m := NewCircuitBreakerMiddleware(5, 3)
	m.state.Store(int32(StateHalfOpen))
	m.halfOpenReqs.Store(0)
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

// BenchmarkCircuitBreakerMiddleware_StateTransition 娴嬭瘯鐘舵€佽浆鎹㈡€ц兘
func BenchmarkCircuitBreakerMiddleware_StateTransition(b *testing.B) {
	job := newBenchJob(context.Background(), def.PriorityNormal, "")
	defer job.Release()
	mctx := NewMiddlewareContext(job.GetContext(), job, "test-service")

	b.Run("Closed->Open", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			m := NewCircuitBreakerMiddleware(5, 3)
			// 瑙﹀彂5娆″け璐?
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
			// 瑙﹀彂3娆℃垚鍔?
			for j := 0; j < 3; j++ {
				m.OnComplete(mctx, nil, nil)
			}
		}
	})
}

// ============================================================================
// 鍩哄噯娴嬭瘯锛氫腑闂翠欢閾炬€ц兘
// ============================================================================

// BenchmarkMiddlewareChain_Empty 娴嬭瘯绌轰腑闂翠欢閾?
func BenchmarkMiddlewareChain_Empty(b *testing.B) {
	chain := NewMiddlewareChain()
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

// BenchmarkMiddlewareChain_Single 娴嬭瘯鍗曚釜涓棿浠?
func BenchmarkMiddlewareChain_Single(b *testing.B) {
	m := NewDispatchKeyStatsMiddleware(nil, 10*time.Second, 10, 0)
	chain := NewMiddlewareChain(m)
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

// BenchmarkMiddlewareChain_Multiple 娴嬭瘯澶氫釜涓棿浠?
func BenchmarkMiddlewareChain_Multiple(b *testing.B) {
	stats := NewDispatchKeyStatsMiddleware(nil, 10*time.Second, 10, 0)
	rateLimit := NewRateLimitMiddleware(100000, 10000)
	breaker := NewCircuitBreakerMiddleware(5, 3)

	chain := NewMiddlewareChain(stats, rateLimit, breaker)
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

// BenchmarkMiddlewareChain_OnComplete 娴嬭瘯 OnComplete 鎬ц兘
func BenchmarkMiddlewareChain_OnComplete(b *testing.B) {
	stats := NewDispatchKeyStatsMiddleware(nil, 10*time.Second, 10, 0)
	rateLimit := NewRateLimitMiddleware(100000, 10000)
	breaker := NewCircuitBreakerMiddleware(5, 3)

	chain := NewMiddlewareChain(stats, rateLimit, breaker)
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

// ============================================================================
// 鍩哄噯娴嬭瘯锛氬苟鍙戠珵浜夊満鏅?
// ============================================================================

// BenchmarkCircuitBreakerMiddleware_ConcurrentStateChange 娴嬭瘯骞跺彂鐘舵€佽浆鎹?
func BenchmarkCircuitBreakerMiddleware_ConcurrentStateChange(b *testing.B) {
	m := NewCircuitBreakerMiddleware(5, 3, WithCooldownDuration(100*time.Microsecond))

	var successCount atomic.Uint64
	var failCount atomic.Uint64

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		job := newBenchJob(context.Background(), def.PriorityNormal, "")
		defer job.Release()
		mctx := NewMiddlewareContext(job.GetContext(), job, "test-service")
		for pb.Next() {
			result := m.OnReceive(mctx)
			if result.Action == def.ActionContinue {
				successCount.Add(1)
				// 妯℃嫙50%澶辫触鐜?
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

// BenchmarkRateLimitMiddleware_Contention 娴嬭瘯闄愭祦绔炰簤鍦烘櫙
func BenchmarkRateLimitMiddleware_Contention(b *testing.B) {
	m := NewRateLimitMiddleware(10000, 1000) // 闄嶄綆閫熺巼浠ヨЕ鍙戦檺娴?

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

	b.Logf("Accepted: %d, Rejected: %d, Rate: %.2f%%",
		accepted.Load(), rejected.Load(),
		float64(accepted.Load())*100/float64(accepted.Load()+rejected.Load()))
}

// ============================================================================
// 鍩哄噯娴嬭瘯锛氬唴瀛樺垎閰?
// ============================================================================

// BenchmarkMiddlewareContext_Creation 娴嬭瘯涓棿浠朵笂涓嬫枃鍒涘缓
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

// BenchmarkMiddlewareContext_SetGet 娴嬭瘯涓婁笅鏂囨暟鎹瓨鍙?
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

// ============================================================================
// 鍩哄噯娴嬭瘯锛氱湡瀹炲満鏅ā鎷?
// ============================================================================

// BenchmarkRealisticWorkload 妯℃嫙鐪熷疄宸ヤ綔璐熻浇
func BenchmarkRealisticWorkload(b *testing.B) {
	// 鍒涘缓瀹屾暣鐨勪腑闂翠欢閾?
	stats := NewDispatchKeyStatsMiddleware(nil, 10*time.Second, 10, 0)
	rateLimit := NewRateLimitMiddleware(100000, 10000)
	breaker := NewCircuitBreakerMiddleware(10, 5)
	chain := NewMiddlewareChain(stats, rateLimit, breaker)

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		job := newBenchJob(ctx, def.PriorityNormal, "")
		defer job.Release()
		i := 0
		for pb.Next() {
			// 妯℃嫙涓嶅悓鐨?dispatcherKey锛?000涓笉鍚岀殑key锛?
			job.SetDispatcherKey("user-" + itoa(i%1000))
			job.SetPriority(def.PriorityNormal)

			// 鎵ц涓棿浠堕摼
			result, mctx := chain.ExecuteOnReceive(job, "test-service")
			if result.Action == def.ActionContinue {
				// 妯℃嫙5%鐨勫け璐ョ巼
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

// BenchmarkHighThroughputScenario 楂樺悶鍚愬満鏅?
func BenchmarkHighThroughputScenario(b *testing.B) {
	// 鍙娇鐢?DispatchKey 缁熻锛屾ā鎷熼珮鍚炲悙鍦烘櫙
	stats := NewDispatchKeyStatsMiddleware(nil, 10*time.Second, 10, 0)
	chain := NewMiddlewareChain(stats)

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

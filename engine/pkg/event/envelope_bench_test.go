package event

import (
	"sync"
	"testing"

	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
)

// ============================================================================
// 模拟当前方案：直接实现 IEvent
// ============================================================================

// mockTimer 模拟 Timer 直接实现 IEvent
type mockTimer struct {
	dto.DataRef
	id   uint64
	name string
}

func (t *mockTimer) GetType() int32            { return int32(ServiceTimerCallback) }
func (t *mockTimer) GetPriority() def.Priority { return def.PriorityNormal }
func (t *mockTimer) GetDispatcherKey() string  { return "" }
func (t *mockTimer) Release()                  { mockTimerPool.Put(t) }

var mockTimerPool = sync.Pool{
	New: func() interface{} { return &mockTimer{} },
}

func newMockTimer(id uint64, name string) *mockTimer {
	t := mockTimerPool.Get().(*mockTimer)
	t.Ref()
	t.id = id
	t.name = name
	return t
}

// ============================================================================
// 模拟泛型 Envelope 方案
// ============================================================================

// ITimer 简化的 Timer 接口
type ITimer interface {
	GetTimerId() uint64
	GetName() string
	Do() error
}

// simpleTimer 只实现业务接口，不实现 IEvent
type simpleTimer struct {
	dto.DataRef
	id   uint64
	name string
}

func (t *simpleTimer) GetTimerId() uint64 { return t.id }
func (t *simpleTimer) GetName() string    { return t.name }
func (t *simpleTimer) Do() error          { return nil }

var simpleTimerPool = sync.Pool{
	New: func() interface{} { return &simpleTimer{} },
}

func newSimpleTimer(id uint64, name string) *simpleTimer {
	t := simpleTimerPool.Get().(*simpleTimer)
	t.Ref()
	t.id = id
	t.name = name
	return t
}

func releaseSimpleTimer(t *simpleTimer) {
	if t.UnRef() {
		t.id = 0
		t.name = ""
		simpleTimerPool.Put(t)
	}
}

// benchTimerEnvelope 泛型 Envelope 模拟（用于基准测试）
type benchTimerEnvelope struct {
	dto.DataRef
	Type          int32
	Priority      def.Priority
	DispatcherKey string
	Payload       ITimer
}

func (e *benchTimerEnvelope) GetType() int32            { return e.Type }
func (e *benchTimerEnvelope) GetPriority() def.Priority { return e.Priority }
func (e *benchTimerEnvelope) GetDispatcherKey() string  { return e.DispatcherKey }
func (e *benchTimerEnvelope) Release() {
	if e.UnRef() {
		e.Type = 0
		e.Priority = 0
		e.DispatcherKey = ""
		e.Payload = nil
		benchTimerEnvelopePool.Put(e)
	}
}

var benchTimerEnvelopePool = sync.Pool{
	New: func() interface{} { return &benchTimerEnvelope{} },
}

func newBenchTimerEnvelope(timer ITimer, dispatcherKey string) *benchTimerEnvelope {
	e := benchTimerEnvelopePool.Get().(*benchTimerEnvelope)
	e.Ref()
	e.Type = int32(ServiceTimerCallback)
	e.Priority = def.PriorityNormal
	e.DispatcherKey = dispatcherKey
	e.Payload = timer
	return e
}

// ============================================================================
// 模拟现有 Event + Data 方案（你现在 Timer 的做法）
// ============================================================================

// 复用现有的 Event，Data 字段放 timer

// ============================================================================
// 基准测试
// ============================================================================

// BenchmarkDirectIEvent 当前方案：Timer 直接实现 IEvent
func BenchmarkDirectIEvent(b *testing.B) {
	b.Run("Create+Release", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			t := newMockTimer(uint64(i), "test-timer")
			_ = t.GetType()
			_ = t.GetPriority()
			_ = t.GetDispatcherKey()
			t.Release()
		}
	})

	b.Run("Parallel", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			i := uint64(0)
			for pb.Next() {
				t := newMockTimer(i, "test-timer")
				_ = t.GetType()
				_ = t.GetPriority()
				_ = t.GetDispatcherKey()
				t.Release()
				i++
			}
		})
	})
}

// BenchmarkGenericEnvelope 泛型 Envelope 方案
func BenchmarkGenericEnvelope(b *testing.B) {
	b.Run("Create+Release", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			timer := newSimpleTimer(uint64(i), "test-timer")
			env := newBenchTimerEnvelope(timer, "dispatcher-key")
			_ = env.GetType()
			_ = env.GetPriority()
			_ = env.GetDispatcherKey()
			// 模拟处理
			_ = env.Payload.Do()
			// 释放（Envelope 不负责释放 Timer）
			env.Release()
			releaseSimpleTimer(timer)
		}
	})

	b.Run("Parallel", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			i := uint64(0)
			for pb.Next() {
				timer := newSimpleTimer(i, "test-timer")
				env := newBenchTimerEnvelope(timer, "dispatcher-key")
				_ = env.GetType()
				_ = env.GetPriority()
				_ = env.GetDispatcherKey()
				_ = env.Payload.Do()
				env.Release()
				releaseSimpleTimer(timer)
				i++
			}
		})
	})
}

// BenchmarkEventWithData 现有 Event + Data 方案（当前 Timer 实际做法）
func BenchmarkEventWithData(b *testing.B) {
	b.Run("Create+Release", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			timer := newSimpleTimer(uint64(i), "test-timer")
			evt := NewEvent()
			evt.Type = ServiceTimerCallback
			evt.Priority = def.PriorityNormal
			evt.DispatcherKey = "dispatcher-key"
			evt.Data = timer
			_ = evt.GetEventType()
			_ = evt.GetPriority()
			_ = evt.GetDispatcherKey()
			// 模拟处理：需要断言
			t := evt.Data.(ITimer)
			_ = t.Do()
			evt.Release()
			releaseSimpleTimer(timer)
		}
	})

	b.Run("Parallel", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			i := uint64(0)
			for pb.Next() {
				timer := newSimpleTimer(i, "test-timer")
				evt := NewEvent()
				evt.Type = ServiceTimerCallback
				evt.Priority = def.PriorityNormal
				evt.DispatcherKey = "dispatcher-key"
				evt.Data = timer
				_ = evt.GetEventType()
				_ = evt.GetPriority()
				_ = evt.GetDispatcherKey()
				t := evt.Data.(ITimer)
				_ = t.Do()
				evt.Release()
				releaseSimpleTimer(timer)
				i++
			}
		})
	})
}

// BenchmarkPoolOverhead 单独测量 sync.Pool 开销
func BenchmarkPoolOverhead(b *testing.B) {
	b.Run("SinglePool", func(b *testing.B) {
		pool := sync.Pool{New: func() interface{} { return &simpleTimer{} }}
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			obj := pool.Get().(*simpleTimer)
			pool.Put(obj)
		}
	})

	b.Run("DoublePool", func(b *testing.B) {
		pool1 := sync.Pool{New: func() interface{} { return &simpleTimer{} }}
		pool2 := sync.Pool{New: func() interface{} { return &benchTimerEnvelope{} }}
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			obj1 := pool1.Get().(*simpleTimer)
			obj2 := pool2.Get().(*benchTimerEnvelope)
			pool2.Put(obj2)
			pool1.Put(obj1)
		}
	})
}

// BenchmarkTypeAssertion 类型断言开销
func BenchmarkTypeAssertion(b *testing.B) {
	timer := &simpleTimer{id: 1, name: "test"}
	var iface interface{} = timer

	b.Run("InterfaceAssertion", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			t := iface.(ITimer)
			_ = t.GetTimerId()
		}
	})

	b.Run("DirectAccess", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = timer.GetTimerId()
		}
	})
}

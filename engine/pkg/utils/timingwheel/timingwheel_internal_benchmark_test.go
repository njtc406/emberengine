package timingwheel

import (
	"context"
	"sync"
	"testing"
	"time"
)

func benchmarkNoopTask(ctx context.Context, t *Timer, args ...interface{}) error { return nil }

func newBenchmarkScheduler(b *testing.B, tick time.Duration, wheelSize int64, chanSize int) (*TimingWheel, ITimerScheduler) {
	b.Helper()
	tw := NewTimingWheel(tick, wheelSize, nil)
	tw.Start()
	scheduler, err := NewJobScheduler("bench", chanSize, 32, tw, nil)
	if err != nil {
		tw.Stop()
		b.Fatalf("NewJobScheduler failed: %v", err)
	}
	return tw, scheduler
}

func BenchmarkTimingWheel_AddCancel_ShortDelay(b *testing.B) {
	for _, tc := range []struct {
		name      string
		wheelSize int64
	}{
		{name: "wheelSize-64", wheelSize: 64},
		{name: "wheelSize-1000", wheelSize: 1000},
	} {
		b.Run(tc.name, func(b *testing.B) {
			tw, scheduler := newBenchmarkScheduler(b, time.Millisecond, tc.wheelSize, 1)
			defer tw.Stop()
			defer scheduler.Stop()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				id, err := scheduler.AfterFunc(time.Hour, "short", benchmarkNoopTask)
				if err != nil {
					b.Fatalf("AfterFunc failed: %v", err)
				}
				scheduler.CancelTimer(id)
			}
		})
	}
}

func BenchmarkTimingWheel_AddCancel_OverflowDelay(b *testing.B) {
	for _, tc := range []struct {
		name      string
		wheelSize int64
		delay     time.Duration
	}{
		{name: "level-2", wheelSize: 64, delay: 10 * time.Second},
		{name: "level-4-plus", wheelSize: 20, delay: 35 * 24 * time.Hour},
		{name: "large-wheel-35d", wheelSize: 1000, delay: 35 * 24 * time.Hour},
	} {
		b.Run(tc.name, func(b *testing.B) {
			tw, scheduler := newBenchmarkScheduler(b, time.Millisecond, tc.wheelSize, 1)
			defer tw.Stop()
			defer scheduler.Stop()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				id, err := scheduler.AfterFunc(tc.delay, "overflow", benchmarkNoopTask)
				if err != nil {
					b.Fatalf("AfterFunc failed: %v", err)
				}
				scheduler.CancelTimer(id)
			}
		})
	}
}

func BenchmarkTimingWheel_SetTimeOffset_Rebuild(b *testing.B) {
	for _, tc := range []struct {
		name      string
		timers    int
		wheelSize int64
	}{
		{name: "timers-1k", timers: 1000, wheelSize: 64},
		{name: "timers-10k", timers: 10000, wheelSize: 64},
		{name: "timers-10k-overflow", timers: 10000, wheelSize: 20},
	} {
		b.Run(tc.name, func(b *testing.B) {
			tw, scheduler := newBenchmarkScheduler(b, time.Millisecond, tc.wheelSize, tc.timers+1)
			defer tw.Stop()
			defer scheduler.Stop()

			ids := make([]uint64, 0, tc.timers)
			for i := 0; i < tc.timers; i++ {
				delay := time.Duration(1+i%3600) * time.Second
				id, err := scheduler.AfterFunc(delay, "offset", benchmarkNoopTask)
				if err != nil {
					b.Fatalf("AfterFunc failed: %v", err)
				}
				ids = append(ids, id)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tw.SetTimeOffset(time.Duration(i%2) * time.Millisecond)
			}
			b.StopTimer()

			for _, id := range ids {
				scheduler.CancelTimer(id)
			}
			tw.SetTimeOffset(0)
		})
	}
}

func BenchmarkTimingWheel_ExpiredDispatch(b *testing.B) {
	for _, tc := range []struct {
		name     string
		async    bool
		consume  bool
		chanSize int
	}{
		{name: "sync-consumed", consume: true, chanSize: 1024},
		{name: "sync-channel-full", consume: false, chanSize: 1},
		{name: "async", async: true, chanSize: 1},
	} {
		b.Run(tc.name, func(b *testing.B) {
			tw, scheduler := newBenchmarkScheduler(b, time.Millisecond, 64, tc.chanSize)
			defer tw.Stop()
			stopped := false
			defer func() {
				if !stopped {
					scheduler.Stop()
				}
			}()

			var done chan struct{}
			if tc.consume {
				done = make(chan struct{})
				go func() {
					defer close(done)
					for timer := range scheduler.GetTimerCbChannel() {
						_ = timer.Do(context.Background())
					}
				}()
			}

			var wg sync.WaitGroup
			if tc.async {
				wg.Add(b.N)
			}
			task := benchmarkNoopTask
			if tc.async {
				task = func(ctx context.Context, t *Timer, args ...interface{}) error {
					wg.Done()
					return nil
				}
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				t := &Timer{task: task, taskScheduler: scheduler, asyncTask: tc.async}
				t.timerId = tw.genTimerId()
				tw.runTimer(t, false)
			}
			if tc.async {
				wg.Wait()
			}
			b.StopTimer()

			if tc.consume {
				scheduler.Stop()
				stopped = true
				<-done
			}
		})
	}
}

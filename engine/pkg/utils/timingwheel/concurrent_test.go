package timingwheel

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/log"
)

// TestConcurrentTimerStopAndExecute tests concurrent stop and execute
func TestConcurrentTimerStopAndExecute(t *testing.T) {
	logger, err := log.NewDefaultLogger(nil)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	tw := NewTimingWheel(time.Millisecond, 20, log.NewLoggerX(logger, log.Fields{"pkg": "test"}))
	tw.Start()
	defer tw.Stop()

	scheduler := NewJobScheduler("concurrent test", 1000, 10, tw, log.NewLoggerX(logger, log.Fields{"pkg": "concurrent test"}))
	var executedCount atomic.Int32
	var stoppedCount atomic.Int32

	ctx := context.Background()
	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		for tm := range scheduler.GetTimerCbChannel() {
			_ = tm.Do(ctx)
		}
	}()

	// Create 100 timers
	timers := make([]uint64, 100)
	for i := 0; i < 100; i++ {
		tId, err := scheduler.AfterFunc(time.Millisecond*10, "test", func(ctx context.Context, timer *Timer, args ...interface{}) error {
			executedCount.Add(1)
			time.Sleep(time.Millisecond)
			return nil
		})
		if err != nil {
			t.Fatalf("Failed to create timer: %v", err)
		}
		timers[i] = tId
	}

	// Stop 50 timers concurrently
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			time.Sleep(time.Millisecond * 5)
			scheduler.CancelTimer(timers[idx])
			stoppedCount.Add(1)
		}(i)
	}

	wg.Wait()
	time.Sleep(time.Millisecond * 50)

	t.Logf("Executed: %d, Stopped: %d", executedCount.Load(), stoppedCount.Load())

	executed := executedCount.Load()
	stopped := stoppedCount.Load()

	if executed == 0 && stopped == 0 {
		t.Errorf("No tasks executed or stopped")
	}

	if stopped > 50 {
		t.Errorf("Stopped count too high: %d", stopped)
	}

	scheduler.Stop()
	<-consumerDone
}

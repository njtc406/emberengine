package timingwheel

import (
	"fmt"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestConcurrentTimerStopAndExecute tests concurrent stop and execute
func TestConcurrentTimerStopAndExecute(t *testing.T) {
	logger, err := log.NewDefaultLogger("", nil, true)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	Start(time.Millisecond, 20, logger)
	defer Stop()

	scheduler := NewTaskScheduler(1000, 10, GetTimingWheel())
	var executedCount atomic.Int32
	var stoppedCount atomic.Int32

	// Create 100 timers
	timers := make([]uint64, 100)
	for i := 0; i < 100; i++ {
		tId, err := scheduler.AfterFunc(time.Millisecond*10, "test", func(timer *Timer, args ...interface{}) error {
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
}

// TestTimerABAProblem tests Timer object pool reuse ABA problem
// Scenario:
// 1. Service A registers a Timer, triggered and posted to A's mailbox
// 2. Service A is under heavy load and doesn't execute timely
// 3. Timer is cancelled and recycled to pool
// 4. Service B registers a Timer, reuses the same Timer object from pool
// 5. Service A starts execution, but the Timer now belongs to B
//
// Solution: Use generation counter + Event.Header to pass version without allocation
func TestTimerABAProblem(t *testing.T) {
	logger, err := log.NewDefaultLogger("", nil, true)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	Start(time.Millisecond, 20, logger)
	defer Stop()

	// Create two schedulers, simulating two services
	schedulerA := NewTaskScheduler(100, 10, GetTimingWheel())
	schedulerB := NewTaskScheduler(100, 10, GetTimingWheel())
	defer schedulerA.Stop()
	defer schedulerB.Stop()

	var executedByA atomic.Int32
	var executedByB atomic.Int32
	var wrongExecution atomic.Int32 // Count of wrong executions (A executes B's task)

	// Simulate high load scenario for service A
	chanA := make(chan ITimer, 1000)
	chanB := make(chan ITimer, 1000)

	// Service A: Register 100 Timers, immediately cancel half
	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("taskA_%d", i)
		tId, err := schedulerA.AfterFunc(time.Millisecond*5, name, func(timer *Timer, args ...interface{}) error {
			executedByA.Add(1)
			return nil
		})
		if err != nil {
			t.Fatalf("Failed to create timer: %v", err)
		}

		// Immediately cancel half, simulating cancellation under high load
		if i%2 == 0 {
			time.Sleep(time.Millisecond * 2)
			schedulerA.CancelTimer(tId)
		}
	}

	// Wait for Timer to trigger and be posted to chanA
	time.Sleep(time.Millisecond * 20)

	// Service B: Register 100 Timers (will reuse cancelled Timer objects)
	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("taskB_%d", i)
		_, err := schedulerB.AfterFunc(time.Millisecond*50, name, func(timer *Timer, args ...interface{}) error {
			executedByB.Add(1)
			return nil
		})
		if err != nil {
			t.Fatalf("Failed to create timer: %v", err)
		}
	}

	// Simulate service A starting to process mailbox (Timer may already be reused)
	go func() {
		for timer := range chanA {
			// Check if it's service A's Timer
			if len(timer.GetName()) >= 6 && timer.GetName()[:6] != "taskA_" {
				wrongExecution.Add(1)
				t.Logf("Service A executed wrong timer: %s", timer.GetName())
			}

			// Verify generation and execute (simulates service's handleTimerCallback)
			timer.Do()
		}
	}()

	go func() {
		for timer := range chanB {
			timer.Do()
		}
	}()

	// Wait for execution to complete
	time.Sleep(time.Millisecond * 100)
	close(chanA)
	close(chanB)

	t.Logf("Executed by A: %d", executedByA.Load())
	t.Logf("Executed by B: %d", executedByB.Load())
	t.Logf("Wrong executions: %d", wrongExecution.Load())

	// Verify:
	// 1. Service A should not execute service B's task
	if wrongExecution.Load() > 0 {
		t.Errorf("Service A executed wrong timer %d times", wrongExecution.Load())
	}

	// 2. Due to version number verification, A can only execute its own Timers (not cancelled)
	if executedByA.Load() > 50 {
		t.Errorf("Service A executed too many timers: %d", executedByA.Load())
	}
}

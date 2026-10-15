package timingwheel

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestStopDoesNotWaitForCallback(t *testing.T) {
	wheel := NewTimingWheel(time.Millisecond, 20, nil)
	wheel.Start()
	defer wheel.Stop()
	owner, err := NewJobScheduler("callback", 10, 2, wheel, nil)
	if err != nil {
		t.Fatal(err)
	}
	entered, release, exited := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer close(release)
	_, err = owner.AfterAsyncFunc(0, "blocked", func(context.Context, *Timer, ...interface{}) error {
		close(entered)
		<-release
		close(exited)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("callback did not start")
	}
	done := make(chan struct{})
	go func() { owner.Stop(); wheel.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop waited for callback")
	}
	select {
	case <-exited:
		t.Fatal("callback unexpectedly finished")
	default:
	}
}

func TestConcurrentStartRegisterAdjustStop(t *testing.T) {
	wheel := NewTimingWheel(time.Millisecond, 20, nil)
	owner, err := NewJobScheduler("concurrent", 100, 4, wheel, nil)
	if err != nil {
		t.Fatal(err)
	}
	scheduler := owner.(*jobScheduler)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for iteration := 0; iteration < 20; iteration++ {
				wheel.Start()
				id, _ := owner.AfterFunc(time.Hour, "pending", nil)
				wheel.SetTimeOffset(time.Duration(iteration) * time.Millisecond)
				owner.CancelTimer(id)
			}
		}()
	}
	workers.Add(1)
	go func() { defer workers.Done(); <-start; wheel.Stop(); owner.Stop() }()
	close(start)
	done := make(chan struct{})
	go func() { workers.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("lifecycle operations blocked")
	}
	wheel.Start()
	wheel.Stop()
	if len(wheel.collectAllTimers()) != 0 {
		t.Fatal("Stop retained bucket timers")
	}
	wheel.timers.Range(func(key, value interface{}) bool { t.Error("Stop retained timer ownership"); return false })
	for _, shard := range scheduler.shards {
		shard.Lock()
		count := len(shard.tasks)
		shard.Unlock()
		if count != 0 {
			t.Errorf("Stop retained %d scheduler timers", count)
		}
	}
}

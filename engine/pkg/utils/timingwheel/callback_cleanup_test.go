package timingwheel

import (
	"testing"
	"time"
)

func TestSchedulerStopDrainsCancelledCallbacks(t *testing.T) {
	wheel := NewTimingWheel(time.Millisecond, 20, nil)
	defer wheel.Stop()
	owner, err := NewJobScheduler("drain", 10, 2, wheel, nil)
	if err != nil {
		t.Fatal(err)
	}
	scheduler := owner.(*jobScheduler)
	timer := scheduler.createTimer()
	timer.taskScheduler = scheduler
	if !scheduler.add(timer) || !scheduler.postTimer(timer) {
		t.Fatal("failed to stage callback")
	}
	scheduler.Stop()
	if len(scheduler.c) != 0 {
		t.Fatal("Stop retained cancelled callback references")
	}
}

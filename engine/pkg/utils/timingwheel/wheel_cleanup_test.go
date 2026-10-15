package timingwheel

import (
	"testing"
	"time"
)

func TestBucketRejectsCancelledTimer(t *testing.T) {
	bucket := newBucket()
	timer := &Timer{}
	timer.stop()
	bucket.Add(timer)
	if bucket.timers.Len() != 0 || timer.getBucket() != nil {
		t.Fatal("cancelled timer was attached to bucket")
	}
}

func TestWheelStopReleasesTimers(t *testing.T) {
	wheel := NewTimingWheel(time.Millisecond, 20, nil)
	owner, err := NewJobScheduler("cleanup", 10, 2, wheel, nil)
	if err != nil {
		t.Fatal(err)
	}
	scheduler := owner.(*jobScheduler)
	defer scheduler.Stop()
	for _, delay := range []time.Duration{10 * time.Millisecond, time.Hour} {
		if _, err := scheduler.AfterFunc(delay, "pending", nil); err != nil {
			t.Fatal(err)
		}
	}
	wheel.Stop()
	if timers := wheel.collectAllTimers(); len(timers) != 0 {
		t.Errorf("Stop retained %d active timers", len(timers))
	}
	for _, shard := range scheduler.shards {
		shard.Lock()
		count := len(shard.tasks)
		shard.Unlock()
		if count != 0 {
			t.Errorf("Stop retained %d scheduler entries", count)
		}
	}
}

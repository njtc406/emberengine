package timingwheel

import (
	"testing"
	"time"
)

func TestDirectTimerCancellationReleasesOwnership(t *testing.T) {
	wheel := NewTimingWheel(time.Millisecond, 20, nil)
	defer wheel.Stop()
	timer := &Timer{}
	wheel.AfterFunc(time.Hour, timer)
	timer.stop()
	if _, retained := wheel.timers.Load(timer); retained {
		t.Fatal("cancelled timer retained by wheel")
	}
}

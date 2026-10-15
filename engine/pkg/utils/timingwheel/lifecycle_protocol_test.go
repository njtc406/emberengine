package timingwheel

import (
	"errors"
	"testing"
	"time"
)

func TestRegistrationClosedAndRollback(t *testing.T) {
	for _, closeOwner := range []string{"scheduler", "wheel", "invalid"} {
		t.Run(closeOwner, func(t *testing.T) {
			wheel := NewTimingWheel(time.Millisecond, 20, nil)
			owner, err := NewJobScheduler("test", 10, 2, wheel, nil)
			if err != nil {
				t.Fatal(err)
			}
			scheduler := owner.(*jobScheduler)
			defer wheel.Stop()
			defer scheduler.Stop()
			if closeOwner == "scheduler" {
				scheduler.Stop()
			}
			if closeOwner == "wheel" {
				wheel.Stop()
			}
			duration, spec := time.Hour, "@every 1h"
			if closeOwner == "invalid" {
				duration, spec = 0, "invalid"
			}
			entries := []struct {
				name     string
				register func() (uint64, error)
			}{
				{"after", func() (uint64, error) { return scheduler.AfterFunc(duration, "test", nil) }},
				{"afterAsync", func() (uint64, error) { return scheduler.AfterAsyncFunc(duration, "test", nil) }},
				{"ticker", func() (uint64, error) { return scheduler.TickerFunc(duration, "test", nil) }},
				{"tickerAsync", func() (uint64, error) { return scheduler.TickerAsyncFunc(duration, "test", nil) }},
				{"cron", func() (uint64, error) { return scheduler.CronFunc(spec, "test", nil) }},
				{"cronAsync", func() (uint64, error) { return scheduler.CronAsyncFunc(spec, "test", nil) }},
			}
			for index, entry := range entries {
				if closeOwner == "invalid" && index < 2 {
					continue
				}
				t.Run(entry.name, func(t *testing.T) {
					id, err := entry.register()
					if id != 0 || err == nil {
						t.Errorf("registration = (%d, %v), want zero ID and error", id, err)
					}
					if closeOwner != "invalid" && !errors.Is(err, ErrClosed) {
						t.Errorf("error = %v, want ErrClosed", err)
					}
					for _, shard := range scheduler.shards {
						shard.Lock()
						count := len(shard.tasks)
						shard.Unlock()
						if count != 0 {
							t.Errorf("failed registration retained %d timers", count)
						}
					}
				})
			}
		})
	}
}

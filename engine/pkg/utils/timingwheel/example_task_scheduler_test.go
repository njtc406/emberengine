package timingwheel_test

import (
	"context"
	"fmt"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
)

type EveryScheduler struct {
	Interval time.Duration
}

func (s *EveryScheduler) Next(prev time.Time) time.Time {
	return prev.Add(s.Interval)
}

func printTask(ctx context.Context, t *timingwheel.Timer, args ...interface{}) error {
	_ = ctx
	fmt.Println("task:", t.GetName())
	return nil
}

func Example_scheduleTimer() {
	logger, err := log.NewDefaultLogger(nil)
	if err != nil {
		panic(err)
	}
	tw := timingwheel.NewTimingWheel(time.Millisecond, 100, log.NewLoggerX(logger, log.Fields{"pkg": "tw"}))
	tw.Start()
	defer tw.Stop()

	dp, err := timingwheel.NewJobScheduler(
		"example",
		1000,
		10,
		tw,
		log.NewLoggerX(logger, log.Fields{"pkg": "timingwheel_example"}),
	)
	if err != nil {
		panic(err)
	}
	defer dp.Stop()

	_, err = dp.AfterFunc(10*time.Millisecond, "hello", printTask)
	if err != nil {
		panic(err)
	}

	select {
	case job := <-dp.GetTimerCbChannel():
		_ = job.Do(context.Background())
	case <-time.After(2 * time.Second):
		panic("timeout waiting timer callback")
	}

	// Output:
	// task: hello
}

package timingwheel_test

import (
	"context"
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/log"

	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
)

func genD(i int) time.Duration {
	return time.Duration(i%10000) * time.Millisecond
}

var dp timingwheel.ITimerScheduler

func printTask1(ctx context.Context, t *timingwheel.Timer, args ...interface{}) error {
	//fmt.Println(">>>>>>>>>>>>>taskId:", taskId)
	_ = ctx
	_ = t
	return nil
}

func BenchmarkTimingWheel_StartStop(b *testing.B) {
	logger, err := log.NewDefaultLogger(&log.LoggerConf{})
	if err != nil {
		b.Fatalf("Failed to create logger: %v", err)
	}
	timingwheel.Start(time.Millisecond, 200, logger)
	defer timingwheel.Stop()

	dp = timingwheel.NewJobScheduler(
		"benchmark test",
		10000000,
		10,
		timingwheel.GetTimingWheel(),
		log.NewLoggerX(logger, log.Fields{"pkg": "timingwheel_benchmark"}),
	)

	cases := []struct {
		name string
		N    int // the data size (i.e. number of existing timers)
	}{
		{"N-1m", 1000000},
		{"N-5m", 5000000},
		{"N-10m", 10000000},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			base := make([]uint64, c.N)
			for i := 0; i < len(base); i++ {
				base[i], err = dp.AfterFunc(genD(i), "", printTask1)
			}
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				tId, _ := dp.AfterFunc(time.Second, "", printTask1)
				dp.CancelTimer(tId)
			}

			b.StopTimer()
			for i := 0; i < len(base); i++ {
				dp.CancelTimer(base[i])
			}
		})
	}
}

func BenchmarkStandardTimer_StartStop(b *testing.B) {
	cases := []struct {
		name string
		N    int // the data size (i.e. number of existing timers)
	}{
		{"N-1m", 1000000},
		{"N-5m", 5000000},
		{"N-10m", 10000000},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			base := make([]*time.Timer, c.N)
			for i := 0; i < len(base); i++ {
				base[i] = time.AfterFunc(genD(i), func() {})
			}
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				time.AfterFunc(time.Second, func() {}).Stop()
			}

			b.StopTimer()
			for i := 0; i < len(base); i++ {
				base[i].Stop()
			}
		})
	}
}

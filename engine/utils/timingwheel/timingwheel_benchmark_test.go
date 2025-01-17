package timingwheel_test

import (
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/utils/timingwheel"
)

func genD(i int) time.Duration {
	return time.Duration(i%10000) * time.Millisecond
}

var dp = timingwheel.NewTaskScheduler(10000000, 1)

func printTask1(taskId uint64, args ...interface{}) {
	//fmt.Println(">>>>>>>>>>>>>taskId:", taskId)
}

func BenchmarkTimingWheel_StartStop(b *testing.B) {
	timingwheel.Start(time.Millisecond, 20)
	defer timingwheel.Stop()

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
			base := make([]*timingwheel.Timer, c.N)
			for i := 0; i < len(base); i++ {
				base[i] = dp.AfterFunc(genD(i), printTask1, nil, nil)
			}
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				dp.AfterFunc(time.Second, printTask1, nil, nil).Stop()
			}

			b.StopTimer()
			for i := 0; i < len(base); i++ {
				base[i].Stop()
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

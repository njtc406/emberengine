package boundedmpsc

import (
	"github.com/njtc406/emberengine/engine/internal/perftest"
	"runtime"
	"sync"
	"testing"
	"time"
)

type latencyMessage struct {
	first, published         time.Time
	push, admission, transit time.Duration
}

func TestQueueLatency(t *testing.T) {
	if testing.Short() {
		t.Skip("fixed-sample latency experiment")
	}
	t.Logf("machine=%+v capacity=4096 payload=interface-pointer warmup=4096 samples=32768", perftest.Machine())
	names := []string{"UnboundedMPSC", "MutexRing", "Channel"}
	for round := 0; round < 5; round++ {
		for offset := 0; offset < len(names); offset++ {
			name := names[(round+offset)%len(names)]
			for _, producers := range []int{1, 2, 4, runtime.GOMAXPROCS(0)} {
				q := perfQueue[any](name)
				run := func(n int) ([]latencyMessage, time.Duration) {
					samples := make([]latencyMessage, n)
					start := make(chan struct{})
					var ready, done sync.WaitGroup
					ready.Add(producers + 1)
					done.Add(producers + 1)
					for p := 0; p < producers; p++ {
						go func(p int) {
							defer done.Done()
							ready.Done()
							<-start
							for i := p; i < n; i += producers {
								s := &samples[i]
								s.first = time.Now()
								for {
									s.published = time.Now()
									if q.Push(s) {
										s.push = time.Since(s.published)
										s.admission = time.Since(s.first)
										break
									}
									runtime.Gosched()
								}
							}
						}(p)
					}
					go func() {
						defer done.Done()
						ready.Done()
						<-start
						for i := 0; i < n; {
							v, ok := q.Pop()
							if !ok {
								runtime.Gosched()
								continue
							}
							s := v.(*latencyMessage)
							s.transit = time.Since(s.published)
							i++
						}
					}()
					ready.Wait()
					before, err := processCPUTime()
					if err != nil {
						t.Fatal(err)
					}
					close(start)
					done.Wait()
					after, err := processCPUTime()
					if err != nil {
						t.Fatal(err)
					}
					return samples, after - before
				}
				run(4096)
				samples, cpu := run(32768)
				push, admission, transit := make([]time.Duration, len(samples)), make([]time.Duration, len(samples)), make([]time.Duration, len(samples))
				for i, s := range samples {
					push[i], admission[i], transit[i] = s.push, s.admission, s.transit
				}
				t.Logf("round=%d candidate=%s producers=%d cpu=%s successful-push=%+v first-attempt-to-accepted=%+v push-to-pop=%+v", round, name, producers, cpu, perftest.Percentiles(push), perftest.Percentiles(admission), perftest.Percentiles(transit))
			}
			if name != "UnboundedMPSC" {
				q := perfQueue[int](name)
				for i := 0; i < 4096; i++ {
					q.Push(i)
				}
				v := make([]time.Duration, 32768)
				for i := range v {
					start := time.Now()
					if q.Push(i) {
						t.Fatal("full push accepted")
					}
					v[i] = time.Since(start)
				}
				t.Logf("round=%d candidate=%s full-rejection=%+v", round, name, perftest.Percentiles(v))
			}
		}
	}
}

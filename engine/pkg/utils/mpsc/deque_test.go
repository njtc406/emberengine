package mpsc

import (
	"fmt"
	"log"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueue_PushPop(t *testing.T) {
	q := New[int]()

	q.Push(1)
	q.Push(2)
	t.Log(q.Pop())
	t.Log(q.Pop())
	t.Log(q.Empty())
}

func TestQueue_Empty(t *testing.T) {
	q := New[int]()
	t.Log(q.Empty())
	q.Push(1)
	t.Log(q.Empty())
}

func TestQueue_PushPopOneProducer(t *testing.T) {
	expCount := 100

	var wg sync.WaitGroup
	wg.Add(1)
	q := New[*string]()
	go func() {
		i := 0
		for {
			_, ok := q.Pop()
			if !ok {
				runtime.Gosched()
				continue
			}
			i++
			if i == expCount {
				wg.Done()
				return
			}
		}
	}()

	var val string = "foo"

	for i := 0; i < expCount; i++ {
		q.Push(&val)
	}

	wg.Wait()
}

func TestMpscQueueConsistency(t *testing.T) {
	max := 1000000
	c := runtime.NumCPU() / 2
	cmax := max / c
	var wg sync.WaitGroup
	wg.Add(1)
	q := New[*string]()
	go func() {
		i := 0
		seen := make(map[string]string)
		for {
			r, ok := q.Pop()
			if !ok {
				runtime.Gosched()

				continue
			}
			i++
			s := *r
			_, present := seen[s]
			if present {
				log.Printf("item have already been seen %v", s)
				t.FailNow()
			}
			seen[s] = s
			if i == cmax*c {
				wg.Done()
				return
			}
		}
	}()

	for j := 0; j < c; j++ {
		jj := j
		go func() {
			for i := 0; i < cmax; i++ {
				val := fmt.Sprintf("%v %v", jj, i)
				q.Push(&val)
			}
		}()
	}

	wg.Wait()
	//time.Sleep(500 * time.Millisecond)
	// queue should be empty
	for i := 0; i < 100; i++ {
		r, ok := q.Pop()
		if !ok {
			log.Printf("unexpected result %+v", r)
			t.FailNow()
		}
	}
}

func benchmarkPushPop(count, c int) {
	var wg sync.WaitGroup
	wg.Add(1)
	q := New[*string]()
	go func() {
		i := 0
		for {
			_, ok := q.Pop()
			if !ok {
				time.Sleep(1)
				continue
			}
			i++
			if i == count {
				wg.Done()
				return
			}
		}
	}()

	var val string = "foo"

	for i := 0; i < c; i++ {
		go func(n int) {
			for n > 0 {
				q.Push(&val)
				n--
			}
		}(count / c)
	}

	wg.Wait()
}

func BenchmarkPushPop(b *testing.B) {
	benchmarks := []struct {
		count       int
		concurrency int
	}{
		{
			count:       1000000,
			concurrency: 1,
		},
		{
			count:       1000000,
			concurrency: 2,
		},
		{
			count:       1000000,
			concurrency: 4,
		},
		{
			count:       1000000,
			concurrency: 8,
		},
		{
			count:       1000000,
			concurrency: 16,
		},
	}
	for _, bm := range benchmarks {
		b.Run(fmt.Sprintf("%d_%d", bm.count, bm.concurrency), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				benchmarkPushPop(bm.count, bm.concurrency)
			}
		})
	}
}

func benchmarkChanPushPop(count, c int) {
	var wg sync.WaitGroup
	wg.Add(1)
	q := make(chan string, 100)
	go func() {
		i := 0
		for {
			select {
			case <-q:
				i++
				if i == count {
					wg.Done()
					return
				}
			}

		}
	}()

	var val = "foo"

	for i := 0; i < c; i++ {
		go func(n int) {
			for n > 0 {
				q <- val
				n--
			}
		}(count / c)
	}

	wg.Wait()
}

func BenchmarkChanPushPop(b *testing.B) {
	benchmarks := []struct {
		count       int
		concurrency int
	}{
		{
			count:       1000000,
			concurrency: 1,
		},
		{
			count:       1000000,
			concurrency: 2,
		},
		{
			count:       1000000,
			concurrency: 4,
		},
		{
			count:       1000000,
			concurrency: 8,
		},
		{
			count:       1000000,
			concurrency: 16,
		},
	}
	for _, bm := range benchmarks {
		b.Run(fmt.Sprintf("%d_%d", bm.count, bm.concurrency), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				benchmarkChanPushPop(bm.count, bm.concurrency)
			}
		})
	}
}

var g_MailChan = make(chan bool, 1)
var g_bMailIn [8]int64

func benchmarkPushPopActor(count, c int) {
	var wg sync.WaitGroup
	wg.Add(1)
	q := New[*string]()
	go func() {
		i := 0
		for {
			select {
			case <-g_MailChan:
				atomic.StoreInt64(&g_bMailIn[0], 0)
				for _, ok := q.Pop(); ok; _, ok = q.Pop() {
					i++
					if i == count {
						wg.Done()
						return
					}
				}
			}
		}
	}()

	var val string = "foo"

	for i := 0; i < c; i++ {
		go func(n int) {
			for n > 0 {
				q.Push(&val)
				if atomic.LoadInt64(&g_bMailIn[0]) == 0 && atomic.CompareAndSwapInt64(&g_bMailIn[0], 0, 1) {
					g_MailChan <- true
				}
				n--
			}
		}(count / c)
	}

	wg.Wait()
}

func BenchmarkPushPopActor(b *testing.B) {
	benchmarks := []struct {
		count       int
		concurrency int
	}{
		{
			count:       1000000,
			concurrency: 1,
		},
		{
			count:       1000000,
			concurrency: 2,
		},
		{
			count:       1000000,
			concurrency: 4,
		},
		{
			count:       1000000,
			concurrency: 8,
		},
		{
			count:       1000000,
			concurrency: 16,
		},
	}
	for _, bm := range benchmarks {
		b.Run(fmt.Sprintf("%d_%d", bm.count, bm.concurrency), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				benchmarkPushPopActor(bm.count, bm.concurrency)
			}
		})
	}
}

const (
	testCount   = 1000000
	warmupCount = 10000
	producerNum = 4
	consumerNum = 4
	channelSize = 1024
)

func BenchmarkLockFreeQueue(b *testing.B) {
	q := New[int]()
	var wg sync.WaitGroup
	wg.Add(producerNum + consumerNum)

	b.ResetTimer()
	for i := 0; i < producerNum; i++ {
		go func() {
			defer wg.Done()
			for n := 0; n < b.N/producerNum; n++ {
				q.Push(n)
			}
		}()
	}

	for i := 0; i < consumerNum; i++ {
		go func() {
			defer wg.Done()
			for n := 0; n < b.N/consumerNum; n++ {
				q.Pop()
			}
		}()
	}
	wg.Wait()
}

/*
goos: windows
goarch: amd64
pkg: github.com/njtc406/emberengine/engine/pkg/utils/mpsc
cpu: AMD Ryzen 7 2700 Eight-Core Processor
BenchmarkLockFreeQueue
BenchmarkLockFreeQueue-16        7177578               167.2 ns/op
PASS
*/

func BenchmarkChannel(b *testing.B) {
	ch := make(chan int, channelSize)
	var wg sync.WaitGroup
	wg.Add(producerNum + consumerNum)

	b.ResetTimer()
	for i := 0; i < producerNum; i++ {
		go func() {
			defer wg.Done()
			for n := 0; n < b.N/producerNum; n++ {
				ch <- n
			}
		}()
	}

	for i := 0; i < consumerNum; i++ {
		go func() {
			defer wg.Done()
			for n := 0; n < b.N/consumerNum; n++ {
				<-ch
			}
		}()
	}
	wg.Wait()
	close(ch)
}

/*
goos: windows
goarch: amd64
pkg: github.com/njtc406/emberengine/engine/pkg/utils/mpsc
cpu: AMD Ryzen 7 2700 Eight-Core Processor
BenchmarkChannel
BenchmarkChannel-16      3479059               354.5 ns/op
PASS
*/

// BenchmarkChannel_MPSC 对比 Go channel 的多生产者单消费者性能
func BenchmarkChannel_MPSC(b *testing.B) {
	ch := make(chan int, 1024)
	var wg sync.WaitGroup
	producers := 4 // 生产者数量
	wg.Add(producers)

	b.ResetTimer()

	// 启动多个生产者
	for p := 0; p < producers; p++ {
		go func() {
			defer wg.Done()
			for i := 0; i < b.N/producers; i++ {
				ch <- i
			}
		}()
	}

	// 单个消费者
	for i := 0; i < b.N; i++ {
		v := <-ch
		_ = v
	}

	wg.Wait()
}

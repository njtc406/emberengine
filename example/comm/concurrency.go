// Package comm
// @Title  title
// @Description  desc
// @Author  yr  2025/7/16
// @Update  yr  2025/7/16
package comm

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/njtc406/emberengine/engine/pkg/core"
	"github.com/njtc406/emberengine/engine/pkg/core/rpc"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	"github.com/njtc406/emberengine/engine/pkg/utils/asynclib"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/timelib"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
	"github.com/njtc406/emberengine/engine/pkg/xcontext"
	"github.com/njtc406/emberengine/example/msg"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const (
	ServiceName1 = "ConcurrencyTest"
	ServiceName2 = "ConcurrencyTest1"
)

type ConcurrencyTest struct {
	core.Service

	autoCallTimerId uint64
}

func (s *ConcurrencyTest) OnInit1() error {
	s.OpenConcurrent(100, 1000000)
	var count atomic.Int32
	wg := sync.WaitGroup{}
	concurrentNum := 100000
	wg.Add(concurrentNum)
	var startTime time.Time
	_ = s.AfterFunc(time.Second, "test", func(timer *timingwheel.Timer, args ...interface{}) {
		// 使用协程不断调用
		startTime = timelib.Now()
		for i := 0; i < concurrentNum; i++ {

			s.AsyncDo("concurrency", func() error {
				return s.Select(rpc.WithServiceName(ServiceName2)).Call(nil, "RpcSum", &msg.Msg_Test_Req{A: 1, B: 2}, nil)
				//return s.Select(rpc.WithServiceName(ServiceName2)).Send(nil, "RpcEmptyFun", nil)
			}, func(err error) {
				count.Add(1)
				//log.SysLogger.Debugf("call ConcurrencyTest1.APISum cost:%d ms, count:%d", timelib.Now().Sub(startTime), count.Load())
				wg.Done()
			})
		}
	})

	go func() {
		wg.Wait()
		log.SysLogger.Debugf("call ConcurrencyTest1.APISum cost:%d ms, count:%d", timelib.Now().Sub(startTime).Milliseconds(), count.Load())
		// send 大约耗时 440ms 100000次
		// call 大约耗时 1350ms 100000次
	}()

	return nil
}

func (s *ConcurrencyTest) OnInit() error {
	s.OpenConcurrent(1000, 1000000)

	//total := 100_000
	total := 100000
	//控制一下并发数
	//concurrency := 1
	//concurrency := 100
	concurrency := 500
	//concurrency := 1000
	//concurrency := 5000
	wg := sync.WaitGroup{}
	//testType := "send"
	//testType := "call"
	testType := "asyncCall"
	wg.Add(total)

	var count atomic.Int32
	locker := sync.RWMutex{}
	durations := make([]int64, total)

	var startTime time.Time

	sema := make(chan struct{}, concurrency)

	_ = s.AfterFunc(time.Second*1, "test", func(timer *timingwheel.Timer, args ...interface{}) {
		startTime = timelib.Now()

		go func() {
			for i := 0; i < total; i++ {
				sema <- struct{}{}
				_ = asynclib.Go(func() {
					func(idx int) {
						defer func() {
							<-sema
							wg.Done()
						}()

						start := time.Now()

						var err error
						ctx := xcontext.New(nil)
						ctx.SetHeader(def.DefaultDispatcherKey, uuid.NewString())

						if testType == "send" {
							err = s.Select(rpc.WithServiceName(ServiceName2)).Send(ctx, "RpcEmptyFun", nil)
						} else if testType == "asyncCall" {
							ctx, cancel := context.WithTimeout(ctx, time.Second*3)
							defer cancel()
							_, err = s.Select(rpc.WithServiceName(ServiceName2)).AsyncCall(ctx, "RpcSum", &msg.Msg_Test_Req{A: 1, B: 2}, &dto.AsyncCallParams{Params: []interface{}{start, idx}}, func(data interface{}, err error, params ...interface{}) {
								if err != nil {
									log.SysLogger.Errorf("call error: %v", err)
									return
								}

								// XXX: 特别注意,如果使用异步模式大并发请求,请给调用方足够的处理回包的worker数量,或者将rpc的timeout调大,否则可能会导致调用方超时
								// 回包的处理速度回成为瓶颈

								//resp, ok := data.(*msg.Msg_Test_Resp)
								//if !ok {
								//	log.SysLogger.Errorf("call error: %v", err)
								//	return
								//} else {
								//	log.SysLogger.Debugf("result:%d", resp.Ret)
								//}
								start := params[0].(time.Time)
								idx := params[1].(int)
								duration := time.Since(start).Microseconds()
								locker.Lock()
								durations[idx] = duration
								locker.Unlock()
								count.Add(1)
							})
							return
						} else {
							var result msg.Msg_Test_Resp
							err = s.Select(rpc.WithServiceName(ServiceName2)).Call(ctx, "RpcSum", &msg.Msg_Test_Req{A: 1, B: 2}, &result)
						}

						if err != nil {
							log.SysLogger.Errorf("call error: %v", err)
							return
						}

						duration := time.Since(start).Microseconds()
						locker.Lock()
						durations[idx] = duration
						locker.Unlock()
						count.Add(1)
					}(i)
				})

			}
		}()
	})

	go func() {
		wg.Wait()

		totalCost := time.Since(startTime).Milliseconds()

		time.Sleep(1 * time.Second)

		sort.Slice(durations, func(i, j int) bool {
			return durations[i] < durations[j]
		})

		fmt.Println("======== RPC Bench Result ========")
		fmt.Printf("Total requests  : %d\n", total)
		fmt.Printf("Concurrency Num : %d\n", concurrency)
		fmt.Printf("Test type       : %s\n", testType)
		fmt.Printf("Total time      : %d ms\n", totalCost)
		fmt.Printf("Avg time per op : %.2f μs\n", float64(totalCost*1000)/float64(total))
		fmt.Printf("QPS             : %d\n", total*1000/int(totalCost))
		fmt.Printf("P50 latency     : %d μs\n", durations[total*50/100])
		fmt.Printf("P90 latency     : %d μs\n", durations[total*90/100])
		fmt.Printf("P99 latency     : %d μs\n", durations[total*99/100])
		fmt.Println("==================================")

		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		fmt.Println("======== Runtime Stats ============")
		fmt.Printf("Goroutines       : %d\n", runtime.NumGoroutine())
		fmt.Printf("GC Total         : %d\n", m.NumGC)
		fmt.Printf("Heap Alloc       : %.2f MB\n", float64(m.HeapAlloc)/1024/1024)
		fmt.Printf("Total Alloc      : %.2f MB\n", float64(m.TotalAlloc)/1024/1024)
		fmt.Printf("Sys Memory       : %.2f MB\n", float64(m.Sys)/1024/1024)
		fmt.Printf("Last GC Pause    : %.2f ms\n", float64(m.PauseNs[(m.NumGC+255)%256])/1e6)
		fmt.Printf("Total GC Pause   : %.2f s\n", float64(m.PauseTotalNs)/1e9)
		fmt.Println("==================================")

		//samples := []metrics.Sample{
		//	{Name: "/gc/heap/allocs:bytes"},           // 当前堆内存分配
		//	{Name: "/sched/goroutines:goroutines"},    // 当前协程数
		//	{Name: "/memory/classes/heap/free:bytes"}, // 未使用的堆内存
		//}
		//metrics.Read(samples)
		//
		//for _, v := range samples {
		//	fmt.Printf("%s = %v\n", v.Name, v.Value)
		//}

		// 打印缓存池
		for _, v := range s.PoolStats() {
			fmt.Printf("%s\n", v)
		}

		/*
				emmmm,这是个悲伤的故事,电脑百兆带宽,跑满了,所以qps最大只有这么多了

				cpu: AMD Ryzen 7 2700 Eight-Core Processor

				call:
					======== RPC Bench Result ========
					Total requests  : 100000
					Concurrency Num : 100
					Total time      : 8602 ms
					Avg time per op : 86.02 μs
					QPS             : 11625
					P50 latency     : 8380 μs
					P90 latency     : 11673 μs
					P99 latency     : 15675 μs
					==================================
					======== Runtime Stats ============
					Goroutines       : 150
					GC Total         : 13
					Heap Alloc       : 44.57 MB
					Total Alloc      : 416.55 MB
					Sys Memory       : 105.27 MB
					Last GC Pause    : 0.00 ms
					Total GC Pause   : 0.00 s
					==================================
					pool_name: rpcMsgPool-perpPool, hit: 196586, miss: 3414, current: 3414, total_alloc: 3414, max_observed: 3414, overflow: 0
					pool_name: metaPool, hit: 0, miss: 134, current: 0, total_alloc: 134, max_observed: 0, overflow: 0
					pool_name: msgEnvelopePool, hit: 0, miss: 133, current: 0, total_alloc: 133, max_observed: 0, overflow: 0
					pool_name: timerPool, hit: 0, miss: 123, current: 0, total_alloc: 123, max_observed: 0, overflow: 0
					pool_name: eventPool, hit: 0, miss: 2, current: 0, total_alloc: 2, max_observed: 0, overflow: 0

					======== RPC Bench Result ========
					Total requests  : 100000
					Concurrency Num : 500
					Total time      : 8520 ms
					Avg time per op : 85.20 μs
					QPS             : 11737
					P50 latency     : 42508 μs
					P90 latency     : 49009 μs
					P99 latency     : 53356 μs
					==================================
					======== Runtime Stats ============
					Goroutines       : 549
					GC Total         : 12
					Heap Alloc       : 76.81 MB
					Total Alloc      : 416.70 MB
					Sys Memory       : 117.27 MB
					Last GC Pause    : 0.00 ms
					Total GC Pause   : 0.00 s
					==================================
					pool_name: rpcMsgPool-perpPool, hit: 196433, miss: 3567, current: 3567, total_alloc: 3567, max_observed: 3567, overflow: 0
					pool_name: metaPool, hit: 0, miss: 521, current: 0, total_alloc: 521, max_observed: 0, overflow: 0
					pool_name: msgEnvelopePool, hit: 0, miss: 519, current: 0, total_alloc: 519, max_observed: 0, overflow: 0
					pool_name: timerPool, hit: 0, miss: 518, current: 0, total_alloc: 518, max_observed: 0, overflow: 0
					pool_name: eventPool, hit: 0, miss: 2, current: 0, total_alloc: 2, max_observed: 0, overflow: 0

					======== RPC Bench Result ========
					Total requests  : 100000
					Concurrency Num : 1000
					Total time      : 8534 ms
					Avg time per op : 85.34 μs
					QPS             : 11717
					P50 latency     : 86513 μs
					P90 latency     : 93516 μs
					P99 latency     : 100499 μs
					==================================
					======== Runtime Stats ============
					Goroutines       : 1049
					GC Total         : 11
					Heap Alloc       : 85.86 MB
					Total Alloc      : 417.72 MB
					Sys Memory       : 133.36 MB
					Last GC Pause    : 0.50 ms
					Total GC Pause   : 0.00 s
					==================================
					pool_name: rpcMsgPool-perpPool, hit: 196148, miss: 3852, current: 3852, total_alloc: 3852, max_observed: 3852, overflow: 0
					pool_name: metaPool, hit: 0, miss: 1017, current: 0, total_alloc: 1017, max_observed: 0, overflow: 0
					pool_name: msgEnvelopePool, hit: 0, miss: 1017, current: 0, total_alloc: 1017, max_observed: 0, overflow: 0
					pool_name: timerPool, hit: 0, miss: 1022, current: 0, total_alloc: 1022, max_observed: 0, overflow: 0
					pool_name: eventPool, hit: 0, miss: 2, current: 0, total_alloc: 2, max_observed: 0, overflow: 0

					======== RPC Bench Result ========
					Total requests  : 100000
					Concurrency Num : 5000
					Total time      : 8530 ms
					Avg time per op : 85.30 μs
					QPS             : 11723
					P50 latency     : 433945 μs
					P90 latency     : 439584 μs
					P99 latency     : 448861 μs
					==================================
					======== Runtime Stats ============
					Goroutines       : 5051
					GC Total         : 9
					Heap Alloc       : 99.89 MB
					Total Alloc      : 426.43 MB
					Sys Memory       : 262.35 MB
					Last GC Pause    : 0.00 ms
					Total GC Pause   : 0.00 s
					==================================
					pool_name: rpcMsgPool-perpPool, hit: 191606, miss: 8394, current: 7993, total_alloc: 8394, max_observed: 7993, overflow: 401
					pool_name: metaPool, hit: 0, miss: 5020, current: 0, total_alloc: 5020, max_observed: 0, overflow: 0
					pool_name: msgEnvelopePool, hit: 0, miss: 5019, current: 0, total_alloc: 5019, max_observed: 0, overflow: 0
					pool_name: timerPool, hit: 0, miss: 5019, current: 0, total_alloc: 5019, max_observed: 0, overflow: 0
					pool_name: eventPool, hit: 0, miss: 1, current: 0, total_alloc: 1, max_observed: 0, overflow: 0

				send:
					======== RPC Bench Result ========
					Total requests  : 100000
					Concurrency Num : 100
					Total time      : 2453 ms
					Avg time per op : 24.53 μs
					QPS             : 40766
					P50 latency     : 2306 μs
					P90 latency     : 5769 μs
					P99 latency     : 9684 μs
					==================================
					======== Runtime Stats ============
					Goroutines       : 151
					GC Total         : 7
					Heap Alloc       : 49.35 MB
					Total Alloc      : 206.62 MB
					Sys Memory       : 105.27 MB
					Last GC Pause    : 0.00 ms
					Total GC Pause   : 0.00 s
					==================================
					pool_name: rpcMsgPool-perpPool, hit: 97959, miss: 2041, current: 2041, total_alloc: 2041, max_observed: 2041, overflow: 0
					pool_name: metaPool, hit: 0, miss: 114, current: 0, total_alloc: 114, max_observed: 0, overflow: 0
					pool_name: msgEnvelopePool, hit: 0, miss: 112, current: 0, total_alloc: 112, max_observed: 0, overflow: 0
					pool_name: timerPool, hit: 0, miss: 1, current: 0, total_alloc: 1, max_observed: 0, overflow: 0
					pool_name: eventPool, hit: 0, miss: 2, current: 0, total_alloc: 2, max_observed: 0, overflow: 0

					======== RPC Bench Result ========
					Total requests  : 100000
					Concurrency Num : 500
					Total time      : 2473 ms
					Avg time per op : 24.73 μs
					QPS             : 40436
					P50 latency     : 23 μs
					P90 latency     : 28274 μs
					P99 latency     : 53848 μs
					==================================
					======== Runtime Stats ============
					Goroutines       : 552
					GC Total         : 6
					Heap Alloc       : 80.36 MB
					Total Alloc      : 206.99 MB
					Sys Memory       : 109.02 MB
					Last GC Pause    : 0.00 ms
					Total GC Pause   : 0.00 s
					==================================
					pool_name: rpcMsgPool-perpPool, hit: 97983, miss: 2017, current: 2017, total_alloc: 2017, max_observed: 2017, overflow: 0
					pool_name: metaPool, hit: 0, miss: 506, current: 0, total_alloc: 506, max_observed: 0, overflow: 0
					pool_name: msgEnvelopePool, hit: 0, miss: 507, current: 0, total_alloc: 507, max_observed: 0, overflow: 0
					pool_name: timerPool, hit: 0, miss: 1, current: 0, total_alloc: 1, max_observed: 0, overflow: 0
					pool_name: eventPool, hit: 0, miss: 1, current: 0, total_alloc: 1, max_observed: 0, overflow: 0

					======== RPC Bench Result ========
					Total requests  : 100000
					Concurrency Num : 1000
					Total time      : 2481 ms
					Avg time per op : 24.81 μs
					QPS             : 40306
					P50 latency     : 13 μs
					P90 latency     : 52332 μs
					P99 latency     : 101425 μs
					==================================
					======== Runtime Stats ============
					Goroutines       : 1051
					GC Total         : 7
					Heap Alloc       : 71.29 MB
					Total Alloc      : 207.63 MB
					Sys Memory       : 113.27 MB
					Last GC Pause    : 0.00 ms
					Total GC Pause   : 0.00 s
					==================================
					pool_name: rpcMsgPool-perpPool, hit: 97797, miss: 2203, current: 2203, total_alloc: 2203, max_observed: 2203, overflow: 0
					pool_name: metaPool, hit: 0, miss: 1010, current: 0, total_alloc: 1010, max_observed: 0, overflow: 0
					pool_name: msgEnvelopePool, hit: 0, miss: 1010, current: 0, total_alloc: 1010, max_observed: 0, overflow: 0
					pool_name: timerPool, hit: 0, miss: 1, current: 0, total_alloc: 1, max_observed: 0, overflow: 0
					pool_name: eventPool, hit: 0, miss: 2, current: 0, total_alloc: 2, max_observed: 0, overflow: 0

					======== RPC Bench Result ========
					Total requests  : 100000
					Concurrency Num : 5000
					Total time      : 2443 ms
					Avg time per op : 24.43 μs
					QPS             : 40933
					P50 latency     : 28 μs
					P90 latency     : 253485 μs
					P99 latency     : 505299 μs
					==================================
					======== Runtime Stats ============
					Goroutines       : 5051
					GC Total         : 6
					Heap Alloc       : 104.51 MB
					Total Alloc      : 213.06 MB
					Sys Memory       : 161.11 MB
					Last GC Pause    : 0.00 ms
					Total GC Pause   : 0.00 s
					==================================
					pool_name: rpcMsgPool-perpPool, hit: 93777, miss: 6223, current: 6223, total_alloc: 6223, max_observed: 6223, overflow: 0
					pool_name: metaPool, hit: 0, miss: 5015, current: 0, total_alloc: 5015, max_observed: 0, overflow: 0
					pool_name: msgEnvelopePool, hit: 0, miss: 5015, current: 0, total_alloc: 5015, max_observed: 0, overflow: 0
					pool_name: timerPool, hit: 0, miss: 1, current: 0, total_alloc: 1, max_observed: 0, overflow: 0
					pool_name: eventPool, hit: 0, miss: 2, current: 0, total_alloc: 2, max_observed: 0, overflow: 0

			cpu: AMD Ryzen 7 5700X 8-Core Processor
					这是另一个配置稍微高点的电脑跑出来的

					======== RPC Bench Result ========
					Total requests  : 100000
					Concurrency Num : 500
					Test type       : send
					Total time      : 207 ms
					Avg time per op : 2.07 μs
					QPS             : 483091
					P50 latency     : 0 μs
					P90 latency     : 2502 μs
					P99 latency     : 5004 μs
					==================================
					======== Runtime Stats ============
					Goroutines       : 1562
					GC Total         : 12
					Heap Alloc       : 45.94 MB
					Total Alloc      : 247.74 MB
					Sys Memory       : 93.14 MB
					Last GC Pause    : 0.00 ms
					Total GC Pause   : 0.00 s
					==================================
					pool_name: rpcMsgPool-perpPool, hit: 97484, miss: 2516, current: 2516, total_alloc: 2516, max_observed: 2516, overflow: 0
					pool_name: metaPool, hit: 0, miss: 514, current: 0, total_alloc: 514, max_observed: 0, overflow: 0
					pool_name: msgEnvelopePool, hit: 0, miss: 514, current: 0, total_alloc: 514, max_observed: 0, overflow: 0
					pool_name: timerPool, hit: 0, miss: 1, current: 0, total_alloc: 1, max_observed: 0, overflow: 0
					pool_name: eventPool, hit: 0, miss: 2, current: 0, total_alloc: 2, max_observed: 0, overflow: 0
					pool_name: bytePool_32KB, hit: 0, miss: 61, current: 0, total_alloc: 61, max_observed: 0, overflow: 0
					pool_name: bytePool_64KB, hit: 0, miss: 0, current: 0, total_alloc: 0, max_observed: 0, overflow: 0
					pool_name: bytePool_128KB, hit: 0, miss: 0, current: 0, total_alloc: 0, max_observed: 0, overflow: 0
					pool_name: bytePool_512KB, hit: 0, miss: 0, current: 0, total_alloc: 0, max_observed: 0, overflow: 0
					pool_name: bytePool_1024KB, hit: 0, miss: 0, current: 0, total_alloc: 0, max_observed: 0, overflow: 0
					pool_name: bytePool_2048KB, hit: 0, miss: 0, current: 0, total_alloc: 0, max_observed: 0, overflow: 0

					======== RPC Bench Result ========
					Total requests  : 100000
					Concurrency Num : 500
					Test type       : call
					Total time      : 1266 ms
					Avg time per op : 12.66 μs
					QPS             : 78988
					P50 latency     : 6005 μs
					P90 latency     : 8507 μs
					P99 latency     : 11510 μs
					==================================
					======== Runtime Stats ============
					Goroutines       : 1549
					GC Total         : 21
					Heap Alloc       : 51.52 MB
					Total Alloc      : 474.16 MB
					Sys Memory       : 93.14 MB
					Last GC Pause    : 0.00 ms
					Total GC Pause   : 0.00 s
					==================================
					pool_name: rpcMsgPool-perpPool, hit: 198058, miss: 1942, current: 1942, total_alloc: 1942, max_observed: 1942, overflow: 0
					pool_name: metaPool, hit: 0, miss: 536, current: 0, total_alloc: 536, max_observed: 0, overflow: 0
					pool_name: msgEnvelopePool, hit: 0, miss: 535, current: 0, total_alloc: 535, max_observed: 0, overflow: 0
					pool_name: timerPool, hit: 0, miss: 519, current: 0, total_alloc: 519, max_observed: 0, overflow: 0
					pool_name: eventPool, hit: 0, miss: 2, current: 0, total_alloc: 2, max_observed: 0, overflow: 0
					pool_name: bytePool_32KB, hit: 0, miss: 77, current: 0, total_alloc: 77, max_observed: 0, overflow: 0
					pool_name: bytePool_64KB, hit: 0, miss: 0, current: 0, total_alloc: 0, max_observed: 0, overflow: 0
					pool_name: bytePool_128KB, hit: 0, miss: 0, current: 0, total_alloc: 0, max_observed: 0, overflow: 0
					pool_name: bytePool_512KB, hit: 0, miss: 0, current: 0, total_alloc: 0, max_observed: 0, overflow: 0
					pool_name: bytePool_1024KB, hit: 0, miss: 0, current: 0, total_alloc: 0, max_observed: 0, overflow: 0
					pool_name: bytePool_2048KB, hit: 0, miss: 0, current: 0, total_alloc: 0, max_observed: 0, overflow: 0

					======== RPC Bench Result ========
					Total requests  : 100000
					Concurrency Num : 500
					Test type       : asyncCall
					Total time      : 481 ms
					Avg time per op : 4.81 μs
					QPS             : 207900
					P50 latency     : 593027 μs
					P90 latency     : 709610 μs
					P99 latency     : 722622 μs
					==================================
					======== Runtime Stats ============
					Goroutines       : 1552
					GC Total         : 11
					Heap Alloc       : 176.29 MB
					Total Alloc      : 586.79 MB
					Sys Memory       : 417.12 MB
					Last GC Pause    : 0.00 ms
					Total GC Pause   : 0.00 s
					==================================
					pool_name: rpcMsgPool-perpPool, hit: 197951, miss: 2049, current: 2049, total_alloc: 2049, max_observed: 2049, overflow: 0
					pool_name: metaPool, hit: 0, miss: 97323, current: 0, total_alloc: 97323, max_observed: 0, overflow: 0
					pool_name: msgEnvelopePool, hit: 0, miss: 97323, current: 0, total_alloc: 97323, max_observed: 0, overflow: 0
					pool_name: timerPool, hit: 0, miss: 97319, current: 0, total_alloc: 97319, max_observed: 0, overflow: 0
					pool_name: eventPool, hit: 0, miss: 2, current: 0, total_alloc: 2, max_observed: 0, overflow: 0
					pool_name: bytePool_32KB, hit: 0, miss: 26, current: 0, total_alloc: 26, max_observed: 0, overflow: 0
					pool_name: bytePool_64KB, hit: 0, miss: 0, current: 0, total_alloc: 0, max_observed: 0, overflow: 0
					pool_name: bytePool_128KB, hit: 0, miss: 0, current: 0, total_alloc: 0, max_observed: 0, overflow: 0
					pool_name: bytePool_512KB, hit: 0, miss: 0, current: 0, total_alloc: 0, max_observed: 0, overflow: 0
					pool_name: bytePool_1024KB, hit: 0, miss: 0, current: 0, total_alloc: 0, max_observed: 0, overflow: 0
					pool_name: bytePool_2048KB, hit: 0, miss: 0, current: 0, total_alloc: 0, max_observed: 0, overflow: 0
		*/
	}()

	return nil
}

func (s *ConcurrencyTest) OnStarted() error {
	// 测试在onstart阶段call其他服务
	//s.callTest()
	return nil
}

func (s *ConcurrencyTest) OnRelease() {
	s.CancelTimer(s.autoCallTimerId)
}

type ConcurrencyTest1Module struct {
	core.Module
}

func (s *ConcurrencyTest1Module) RpcSum(req *msg.Msg_Test_Req) *msg.Msg_Test_Resp {
	//log.SysLogger.Debugf(">>>>>>>>>>> call %s func RpcSum, a:%d, b:%d", s.GetModuleName(), a, b)
	return &msg.Msg_Test_Resp{Ret: req.A * req.B}
}

func (s *ConcurrencyTest1Module) ApiSum(a, b int) int {
	//log.SysLogger.Debugf(">>>>>>>>>>> call %s func ApiSum, a:%d, b:%d", s.GetModuleName(), a, b)
	return a + b
}

type ConcurrencyTest1 struct {
	core.Service
}

func (s *ConcurrencyTest1) OnInit() error {
	_, _ = s.AddModule(&ConcurrencyTest1Module{})
	return nil
}

func (s *ConcurrencyTest1) EmptyFun() {

}

func (s *ConcurrencyTest1) RpcEmptyFun() {

}

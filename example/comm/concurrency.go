// Package comm
// @Title  title
// @Description  desc
// @Author  yr  2025/7/16
// @Update  yr  2025/7/16
package comm

import (
	"github.com/njtc406/emberengine/engine/pkg/core"
	"github.com/njtc406/emberengine/engine/pkg/core/rpc"
	"github.com/njtc406/emberengine/engine/pkg/event"
	"github.com/njtc406/emberengine/engine/pkg/utils/asynclib"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/timelib"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
	"github.com/njtc406/emberengine/example/msg"
	"runtime"
	"runtime/metrics"
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

	total := 100_000
	concurrency := 500 // 控制一下并发数
	wg := sync.WaitGroup{}
	wg.Add(total)

	var count atomic.Int32
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

						var result msg.Msg_Test_Resp
						err := s.Select(rpc.WithServiceName(ServiceName2)).Call(nil, "RpcSum", &msg.Msg_Test_Req{A: 1, B: 2}, &result)
						//err := s.Select(rpc.WithServiceName(ServiceName2)).Send(nil, "RpcEmptyFun", nil)
						if err != nil {
							log.SysLogger.Errorf("call error: %v", err)
							return
						}

						duration := time.Since(start).Microseconds()
						durations[idx] = duration
						count.Add(1)
					}(i)
				})

			}
		}()
	})

	go func() {
		wg.Wait()
		totalCost := time.Since(startTime).Milliseconds()

		sort.Slice(durations, func(i, j int) bool {
			return durations[i] < durations[j]
		})

		s.GetLogger().Debugf("======== RPC Bench Result ========")
		s.GetLogger().Debugf("Total requests  : %d", total)
		s.GetLogger().Debugf("Total time      : %d ms", totalCost)
		s.GetLogger().Debugf("Avg time per op : %.2f μs", float64(totalCost*1000)/float64(total))
		s.GetLogger().Debugf("QPS             : %d", total*1000/int(totalCost))
		s.GetLogger().Debugf("P50 latency     : %d μs", durations[total*50/100])
		s.GetLogger().Debugf("P90 latency     : %d μs", durations[total*90/100])
		s.GetLogger().Debugf("P99 latency     : %d μs", durations[total*99/100])
		s.GetLogger().Debugf("==================================")
		// 100000
		// send:
		// grpc 共消耗 1100 ms  avg time: 11.35 us  QPS: 88105   p50: 5004 us  p90: 10509 us  p99: 19016 us
		// rpcx 共消耗 500 ms   avg time: 5 us      QPS: 200000  p50: 0 us     p90: 3002 us   p99: 16013 us
		// nats 共消耗 204 ms   avg time: 2.04 μs   QPS: 490196  p50: 0 us     p90: 2006 us   p99: 5004 us

		// call:
		// grpc 共消耗 15760 ms avg time: 157.60 us QPS: 6345    p50: 75565 us p90: 85573 us  p99: 118601 us
		// rpcx 共消耗 4093 ms  avg time: 40 us     QPS: 24431   p50: 20017 us p90: 22019 us  p99: 30025 us
		// nats 共消耗 1878 ms  avg time: 18.78 us  QPS: 53248   p50: 9503 us  p90: 11510 us  p99: 14512 us

		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		s.GetLogger().Debugf("==== Runtime Stats ====")
		s.GetLogger().Debugf("Goroutines       : %d", runtime.NumGoroutine())
		s.GetLogger().Debugf("GC Total         : %d", m.NumGC)
		s.GetLogger().Debugf("Heap Alloc       : %.2f MB", float64(m.HeapAlloc)/1024/1024)
		s.GetLogger().Debugf("Total Alloc      : %.2f MB", float64(m.TotalAlloc)/1024/1024)
		s.GetLogger().Debugf("Sys Memory       : %.2f MB", float64(m.Sys)/1024/1024)
		s.GetLogger().Debugf("Last GC Pause    : %.2f ms", float64(m.PauseNs[(m.NumGC+255)%256])/1e6)
		s.GetLogger().Debugf("Total GC Pause   : %.2f s", float64(m.PauseTotalNs)/1e9)
		s.GetLogger().Debugf("========================")

		samples := []metrics.Sample{
			{Name: "/gc/heap/allocs:bytes"},           // 当前堆内存分配
			{Name: "/sched/goroutines:goroutines"},    // 当前协程数
			{Name: "/memory/classes/heap/free:bytes"}, // 未使用的堆内存
		}
		metrics.Read(samples)

		for _, v := range samples {
			s.GetLogger().Debugf("%s = %v\n", v.Name, v.Value)
		}

		// 打印缓存池
		//s.GetLogger().Debug(msgenvelope.GetMsgPoolStats())
		//s.GetLogger().Debug(msgenvelope.GetMetaPoolStats())
		s.GetLogger().Debug(timingwheel.GetTimerPoolStats().String())
		s.GetLogger().Debug(event.GetEventPoolStats().String())
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

type Service2TestModule struct {
	core.Module
}

func (s *Service2TestModule) RpcSum(req *msg.Msg_Test_Req) *msg.Msg_Test_Resp {
	//log.SysLogger.Debugf(">>>>>>>>>>> call %s func RpcSum, a:%d, b:%d", s.GetModuleName(), a, b)
	return &msg.Msg_Test_Resp{Ret: req.A + req.B}
}

func (s *Service2TestModule) ApiSum(a, b int) int {
	//log.SysLogger.Debugf(">>>>>>>>>>> call %s func ApiSum, a:%d, b:%d", s.GetModuleName(), a, b)
	return a + b
}

type ConcurrencyTest1 struct {
	core.Service
}

func (s *ConcurrencyTest1) OnInit() error {
	_, _ = s.AddModule(&Service2TestModule{})
	return nil
}

func (s *ConcurrencyTest1) EmptyFun() {

}

func (s *ConcurrencyTest1) RpcEmptyFun() {

}

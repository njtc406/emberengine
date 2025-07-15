// Package main
// @Title  title
// @Description  desc
// @Author  yr  2024/12/4
// @Update  yr  2024/12/4
package main

import (
	"fmt"
	"github.com/njtc406/emberengine/engine/pkg/core"
	"github.com/njtc406/emberengine/engine/pkg/core/rpc"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/node"
	"github.com/njtc406/emberengine/engine/pkg/services"
	_ "github.com/njtc406/emberengine/engine/pkg/sysService/pprofservice"
	"github.com/njtc406/emberengine/engine/pkg/utils/asynclib"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/timelib"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
	"github.com/njtc406/emberengine/example/msg"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const (
	ServiceName1 = "Service1"
	ServiceName2 = "Service2"
	ServiceName3 = "Service3"
)

type Service1 struct {
	core.Service

	autoCallTimerId uint64
}

func (s *Service1) OnInit1() error {
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
				//log.SysLogger.Debugf("call Service2.APISum cost:%d ms, count:%d", timelib.Now().Sub(startTime), count.Load())
				wg.Done()
			})
		}
	})

	go func() {
		wg.Wait()
		log.SysLogger.Debugf("call Service2.APISum cost:%d ms, count:%d", timelib.Now().Sub(startTime).Milliseconds(), count.Load())
		// send 大约耗时 440ms 100000次
		// call 大约耗时 1350ms 100000次
	}()

	return nil
}

func (s *Service1) OnInit() error {
	s.OpenConcurrent(1000, 1000000)

	total := 100_000
	concurrency := 500
	wg := sync.WaitGroup{}
	wg.Add(total)

	var count atomic.Int32
	durations := make([]int64, total)

	var startTime time.Time

	sema := make(chan struct{}, concurrency)

	_ = s.AfterFunc(time.Second, "test", func(timer *timingwheel.Timer, args ...interface{}) {
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

		fmt.Printf("======== RPC Bench Result ========\n")
		fmt.Printf("Total requests  : %d\n", total)
		fmt.Printf("Total time      : %d ms\n", totalCost)
		fmt.Printf("Avg time per op : %.2f μs\n", float64(totalCost*1000)/float64(total))
		fmt.Printf("QPS             : %d\n", total*1000/int(totalCost))
		fmt.Printf("P50 latency     : %d μs\n", durations[total*50/100])
		fmt.Printf("P90 latency     : %d μs\n", durations[total*90/100])
		fmt.Printf("P99 latency     : %d μs\n", durations[total*99/100])
		fmt.Printf("==================================\n")
	}()

	return nil
}

func (s *Service1) OnStarted() error {
	// 测试在onstart阶段call其他服务
	//s.callTest()
	return nil
}

func (s *Service1) OnRelease() {
	s.CancelTimer(s.autoCallTimerId)
}

func init() {
	services.SetService("Service1", func() inf.IService {
		return &Service1{}
	})
}

func main() {
	runtime.GOMAXPROCS(16) // 匹配CPU核心数
	runtime.SetMutexProfileFraction(1)
	node.Start(node.WithConfPath("./example/configs/node_concurrency"))
}

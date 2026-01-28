// Package comm
// @Title  title
// @Description  desc
// @Author  yr  2025/7/16
// @Update  yr  2025/7/16
package comm

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/core"
	"github.com/njtc406/emberengine/engine/pkg/core/rpc"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/diag"
	"github.com/njtc406/emberengine/engine/pkg/utils/timelib"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
	"github.com/njtc406/emberengine/engine/pkg/utils/xcontext"
	"github.com/njtc406/emberengine/example/msg"
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
	_, _ = s.AfterFunc(time.Second, "test", func(ctx context.Context, timer *timingwheel.Timer, args ...interface{}) error {
		// 使用协程不断调用
		startTime = timelib.Now()
		for i := 0; i < concurrentNum; i++ {

			s.AsyncDo("concurrency", context.Background(), func(ctx context.Context) error {
				return s.Select(rpc.WithName(ServiceName2)).Call(nil, "RpcSum", &msg.Msg_Test_Req{A: 1, B: 2}, nil)
				//return s.Select(rpc.WithName(ServiceName2)).Send(nil, "RpcEmptyFun", nil)
			}, func(ctx context.Context, err error) {
				count.Add(1)
				//log.SysLogger.Debugf("call ConcurrencyTest1.APISum cost:%d ms, count:%d", timelib.Now().Sub(startTime), count.Load())
				wg.Done()
			})
		}
		return nil
	})

	go func() {
		wg.Wait()
		if diag.Enabled() {
			log.SysLogger.Debugf("call ConcurrencyTest1.APISum cost:%d ms, count:%d", timelib.Now().Sub(startTime).Milliseconds(), count.Load())
		}
		// send 大约耗时 440ms 100000次
		// call 大约耗时 1350ms 100000次
	}()

	return nil
}

func (s *ConcurrencyTest) OnInit() error {
	s.OpenConcurrent(1000, 1000000) // 开启并发组件

	//total := 100_000
	total := 100000
	if v := os.Getenv("BENCH_TOTAL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			total = n
		}
	}
	//total := 10000
	//total := 10
	//控制一下并发数
	//concurrency := 1
	//concurrency := 100
	concurrency := 500
	if v := os.Getenv("BENCH_CONCURRENCY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			concurrency = n
		}
	}
	//concurrency := 1000
	//concurrency := 5000
	wg := sync.WaitGroup{}
	benchMode := "workers" // default to higher-throughput mode; set BENCH_MODE=goroutine to use legacy behavior
	if v := os.Getenv("BENCH_MODE"); v != "" {
		benchMode = v
	}
	testType := "send"
	if v := os.Getenv("BENCH_TYPE"); v != "" {
		testType = v
	}
	//testType := "call"
	//testType := "asyncCall"
	wg.Add(total)

	var count atomic.Int32
	var errCount atomic.Int32
	var lastSuccessUnixNano atomic.Int64
	// 注意：total 很大时（例如为了 profiling 拉到几千万），为每个请求记录 duration 会导致巨量内存占用
	// 并且排序计算 p99 成本极高，反过来会污染性能测试与 pprof。
	const maxRecordDurations = 1_000_000
	recordDurations := total > 0 && total <= maxRecordDurations
	recordDurationsNote := ""
	// 显式控制是否记录每次请求耗时（time.Now/time.Since + durations 写入）。
	// 默认保持历史行为：小 total 记录，大 total 自动跳过。
	// 在只关心 QPS 的场景（尤其是 total=10w/100w）建议设置 BENCH_RECORD_DURATIONS=0。
	if v := os.Getenv("BENCH_RECORD_DURATIONS"); v != "" {
		switch v {
		case "0", "false", "FALSE", "off", "OFF":
			recordDurations = false
			recordDurationsNote = "skipped; BENCH_RECORD_DURATIONS=0"
		case "1", "true", "TRUE", "on", "ON":
			recordDurations = true
		}
	}
	if !recordDurations && recordDurationsNote == "" {
		recordDurationsNote = fmt.Sprintf("skipped; total>%d", maxRecordDurations)
	}

	// 是否追踪最后一次成功时间（用于 success time / tail wait 计算）。
	// 该逻辑会在每次成功时调用 time.Now().UnixNano()+atomic 更新，纯 QPS 测试建议关闭。
	trackSuccessTs := true
	if v := os.Getenv("BENCH_TRACK_SUCCESS_TS"); v != "" {
		switch v {
		case "0", "false", "FALSE", "off", "OFF":
			trackSuccessTs = false
		case "1", "true", "TRUE", "on", "ON":
			trackSuccessTs = true
		}
	}

	var durations []int64
	if recordDurations {
		durations = make([]int64, total)
	}

	var startTime time.Time

	_, _ = s.AfterFunc(time.Second*1, "test", func(ctx context.Context, timer *timingwheel.Timer, args ...interface{}) error {
		var keys = make([]string, concurrency)
		for i := 0; i < concurrency; i++ {
			keys[i] = fmt.Sprintf("bench-%d", i)
		}
		startTime = timelib.Now()
		if diag.Enabled() {
			log.SysLogger.WithFields(map[string]interface{}{
				"benchMode":                  benchMode,
				"testType":                   testType,
				"total":                      total,
				"concurrency":                concurrency,
				"recordDurations":            recordDurations,
				"trackSuccessTs":             trackSuccessTs,
				"GOMAXPROCS":                 runtime.GOMAXPROCS(0),
				"NumCPU":                     runtime.NumCPU(),
				"env.GOMAXPROCS":             os.Getenv("GOMAXPROCS"),
				"env.GOGC":                   os.Getenv("GOGC"),
				"env.GOMEMLIMIT":             os.Getenv("GOMEMLIMIT"),
				"env.EMBER_LOG_STDOUT":       os.Getenv("EMBER_LOG_STDOUT"),
				"env.EMBER_NATS_SENDER_POOL": os.Getenv("EMBER_NATS_SENDER_POOL"),
			}).Infof("bench config")
		}

		if benchMode == "workers" {
			var nextIdx atomic.Int64
			workerWg := sync.WaitGroup{}
			workerWg.Add(concurrency)

			// 是否每请求生成新 traceID（默认关闭以获得最高吞吐）
			perReqTrace := false
			if v := os.Getenv("BENCH_PER_REQ_TRACE"); v != "" {
				switch v {
				case "1", "true", "TRUE", "on", "ON":
					perReqTrace = true
				}
			}

			for workerID := 0; workerID < concurrency; workerID++ {
				wid := workerID
				go func() {
					defer workerWg.Done()

					// 使用 ContextFactory 高效创建 context
					factory := xcontext.NewFactory(map[string]any{
						def.DefaultDispatcherKey: keys[wid],
					})

					for {
						idx := int(nextIdx.Add(1) - 1)
						if idx >= total {
							return
						}

						// 根据配置决定是否每请求生成新 traceID
						var ctxx xcontext.XContext
						if perReqTrace {
							ctxx = factory.NewContext() // 每请求新 traceID
						} else {
							ctxx = factory.NewContextWithoutTrace() // 无 traceID，最高吞吐
						}

						var start time.Time
						if recordDurations {
							start = time.Now()
						}
						switch testType {
						case "send":
							err := s.Select(rpc.WithName(ServiceName2)).Send(ctxx, "abc", nil)
							if err != nil {
								errCount.Add(1)
								log.SysLogger.Errorf("call error: %v", err)
							} else {
								if trackSuccessTs {
									updateMaxInt64(&lastSuccessUnixNano, time.Now().UnixNano())
								}
							}
							if recordDurations {
								durations[idx] = time.Since(start).Microseconds()
							}
							count.Add(1)
							wg.Done()

						case "asyncCall":
							callCtx, cancel := context.WithTimeout(ctx, time.Second*100)
							var cbStart interface{}
							if recordDurations {
								cbStart = start
							}
							_, err := s.Select(rpc.WithName(ServiceName2)).AsyncCall(callCtx, "RpcSum", &msg.Msg_Test_Req{A: 1, B: 2},
								&dto.AsyncCallParams{Params: []interface{}{cbStart, idx, cancel}}, func(_ context.Context, _ interface{}, cbErr error, params ...interface{}) {
									defer wg.Done()
									defer params[2].(context.CancelFunc)()

									if cbErr != nil {
										errCount.Add(1)
										log.SysLogger.Errorf("call error: %v", cbErr)
									} else {
										if trackSuccessTs {
											updateMaxInt64(&lastSuccessUnixNano, time.Now().UnixNano())
										}
									}

									idx := params[1].(int)
									if recordDurations {
										st := params[0].(time.Time)
										durations[idx] = time.Since(st).Microseconds()
									}
									count.Add(1)
								})
							if err != nil {
								defer cancel()
								errCount.Add(1)
								log.SysLogger.Errorf("call error: %v", err)
								count.Add(1)
								wg.Done()
							}

						case "userData": // 模拟获取用户数据（更大的请求/响应体）
							var result msg.UserDataResp
							callCtx, cancel := context.WithTimeout(ctx, time.Second*30)
							err := s.Select(rpc.WithName(ServiceName2)).Call(callCtx, "RpcGetUserData", &msg.UserDataReq{
								UserId:   int64(idx),
								Token:    "test-token-12345",
								DataType: 1,
							}, &result)
							cancel()
							if err != nil {
								errCount.Add(1)
								log.SysLogger.Errorf("call error: %v", err)
							} else {
								if trackSuccessTs {
									updateMaxInt64(&lastSuccessUnixNano, time.Now().UnixNano())
								}
							}
							if recordDurations {
								durations[idx] = time.Since(start).Microseconds()
							}
							count.Add(1)
							wg.Done()

						case "battle": // 模拟战斗请求
							var result msg.BattleResp
							callCtx, cancel := context.WithTimeout(ctx, time.Second*30)
							err := s.Select(rpc.WithName(ServiceName2)).Call(callCtx, "RpcBattle", &msg.BattleReq{
								PlayerId: int64(idx),
								TargetId: int64(idx + 1),
								SkillId:  int32(idx % 10),
								Params:   []int32{100, 200},
							}, &result)
							cancel()
							if err != nil {
								errCount.Add(1)
								log.SysLogger.Errorf("call error: %v", err)
							} else {
								if trackSuccessTs {
									updateMaxInt64(&lastSuccessUnixNano, time.Now().UnixNano())
								}
							}
							if recordDurations {
								durations[idx] = time.Since(start).Microseconds()
							}
							count.Add(1)
							wg.Done()

						default: // "call"
							var result msg.Msg_Test_Resp
							callCtx, cancel := context.WithTimeout(ctx, time.Second*30)
							err := s.Select(rpc.WithName(ServiceName2)).Call(callCtx, "RpcSum", &msg.Msg_Test_Req{A: 1, B: 2}, &result)
							cancel()
							if err != nil {
								errCount.Add(1)
								log.SysLogger.Errorf("call error: %v", err)
							} else {
								if trackSuccessTs {
									updateMaxInt64(&lastSuccessUnixNano, time.Now().UnixNano())
								}
							}
							if recordDurations {
								durations[idx] = time.Since(start).Microseconds()
							}
							count.Add(1)
							wg.Done()
						}
					}
				}()
			}
			go func() { workerWg.Wait() }()
			return nil
		}

		// legacy behavior: 1 request -> 1 goroutine; use semaphore to cap in-flight
		sema := make(chan struct{}, concurrency)
		go func() {
			for i := 0; i < total; i++ {
				sema <- struct{}{}
				idx := i
				go func() {
					defer func() {
						<-sema
						wg.Done()
					}()

					var start time.Time
					if recordDurations {
						start = time.Now()
					}
					ctx := xcontext.New(nil)
					ctx.AddHeader(def.DefaultDispatcherKey, keys[idx%concurrency])

					switch testType {
					case "send":
						err := s.Select(rpc.WithName(ServiceName2)).Send(ctx, "abc", nil)
						if err != nil {
							errCount.Add(1)
							log.SysLogger.Errorf("call error: %v", err)
						} else {
							if trackSuccessTs {
								updateMaxInt64(&lastSuccessUnixNano, time.Now().UnixNano())
							}
						}
						if recordDurations {
							durations[idx] = time.Since(start).Microseconds()
						}
						count.Add(1)
						return

					case "asyncCall":
						callCtx, cancel := context.WithTimeout(ctx, time.Second*100)
						var cbStart interface{}
						if recordDurations {
							cbStart = start
						}
						_, err := s.Select(rpc.WithName(ServiceName2)).AsyncCall(callCtx, "RpcSum", &msg.Msg_Test_Req{A: 1, B: 2},
							&dto.AsyncCallParams{Params: []interface{}{cbStart, idx, cancel}}, func(_ context.Context, _ interface{}, cbErr error, params ...interface{}) {
								defer func() {
									<-sema
									wg.Done()
								}()
								defer params[2].(context.CancelFunc)()

								if cbErr != nil {
									errCount.Add(1)
									log.SysLogger.Errorf("call error: %v", cbErr)
								} else {
									if trackSuccessTs {
										updateMaxInt64(&lastSuccessUnixNano, time.Now().UnixNano())
									}
								}

								idx := params[1].(int)
								if recordDurations {
									st := params[0].(time.Time)
									durations[idx] = time.Since(st).Microseconds()
								}
								count.Add(1)
							})
						if err != nil {
							defer cancel()
							errCount.Add(1)
							log.SysLogger.Errorf("call error: %v", err)
							count.Add(1)
							return
						}
						return

					default: // "call"
						var result msg.Msg_Test_Resp
						callCtx, cancel := context.WithTimeout(ctx, time.Second*30)
						err := s.Select(rpc.WithName(ServiceName2)).Call(callCtx, "RpcSum", &msg.Msg_Test_Req{A: 1, B: 2}, &result)
						cancel()
						if err != nil {
							errCount.Add(1)
							log.SysLogger.Errorf("call error: %v", err)
						} else {
							if trackSuccessTs {
								updateMaxInt64(&lastSuccessUnixNano, time.Now().UnixNano())
							}
						}
						if recordDurations {
							durations[idx] = time.Since(start).Microseconds()
						}
						count.Add(1)
						return
					}
				}()
			}
		}()
		return nil
	})

	go func() {
		wg.Wait()

		totalCost := time.Since(startTime).Milliseconds()

		completed := int64(count.Load())
		errors := int64(errCount.Load())
		success := completed - errors
		if success < 0 {
			success = 0
		}

		successCost := totalCost
		tailWait := int64(0)
		if trackSuccessTs {
			startUnixNano := startTime.UnixNano()
			lastOkUnixNano := lastSuccessUnixNano.Load()
			if lastOkUnixNano > startUnixNano {
				successCost = (lastOkUnixNano - startUnixNano) / int64(time.Millisecond)
				if successCost < 1 {
					successCost = 1
				}
			}
			tailWait = totalCost - successCost
			if tailWait < 0 {
				tailWait = 0
			}
		}

		time.Sleep(1 * time.Second)

		if recordDurations {
			sort.Slice(durations, func(i, j int) bool {
				return durations[i] < durations[j]
			})
		}

		fmt.Println("======== RPC Bench Result ========")
		fmt.Printf("Total requests  : %d\n", total)
		fmt.Printf("Concurrency Num : %d\n", concurrency)
		fmt.Printf("Test type       : %s\n", testType)
		fmt.Printf("Total time      : %d ms\n", totalCost)
		fmt.Printf("Completed       : %d (errors: %d)\n", completed, errors)
		fmt.Printf("Success time    : %d ms (tail wait: %d ms)\n", successCost, tailWait)
		fmt.Printf("Avg time per op : %.2f μs\n", float64(totalCost*1000)/float64(total))
		if totalCost > 0 {
			fmt.Printf("QPS (overall)   : %d\n", total*1000/int(totalCost))
		} else {
			fmt.Printf("QPS (overall)   : %d\n", 0)
		}
		if successCost > 0 {
			fmt.Printf("QPS (success)   : %d\n", success*1000/successCost)
		} else {
			fmt.Printf("QPS (success)   : %d\n", 0)
		}
		if recordDurations {
			fmt.Printf("P50 latency     : %d μs\n", durations[total*50/100])
			fmt.Printf("P90 latency     : %d μs\n", durations[total*90/100])
			fmt.Printf("P99 latency     : %d μs\n", durations[total*99/100])
		} else {
			fmt.Printf("P50 latency     : (%s)\n", recordDurationsNote)
			fmt.Printf("P90 latency     : (%s)\n", recordDurationsNote)
			fmt.Printf("P99 latency     : (%s)\n", recordDurationsNote)
		}
		fmt.Println("==================================")

		// 输出 GC 统计帮助诊断抖动
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		fmt.Println("======== GC Stats ================")
		fmt.Printf("GC cycles       : %d\n", m.NumGC)
		fmt.Printf("Total GC pause  : %.2f ms\n", float64(m.PauseTotalNs)/1e6)
		fmt.Printf("Avg GC pause    : %.2f ms\n", float64(m.PauseTotalNs)/float64(m.NumGC+1)/1e6)
		fmt.Printf("Heap Alloc      : %.2f MB\n", float64(m.HeapAlloc)/1024/1024)
		fmt.Printf("Total Alloc     : %.2f MB\n", float64(m.TotalAlloc)/1024/1024)
		fmt.Println("==================================")

		/*
		   ======== RPC Bench Result ========
		   Total requests  : 100000
		   Concurrency Num : 500
		   Test type       : call
		   Total time      : 1237 ms
		   Completed       : 100000 (errors: 0)
		   Success time    : 1237 ms (tail wait: 0 ms)
		   Avg time per op : 12.37 μs
		   QPS (overall)   : 80840
		   QPS (success)   : 80840
		   P50 latency     : 6005 μs
		   P90 latency     : 8006 μs
		   P99 latency     : 10508 μs
		   ==================================
		   ======== GC Stats ================
		   GC cycles       : 5
		   Total GC pause  : 0.50 ms
		   Avg GC pause    : 0.08 ms
		   Heap Alloc      : 27.53 MB
		   Total Alloc     : 368.79 MB
		   ==================================
		*/
	}()

	return nil
}

func updateMaxInt64(target *atomic.Int64, v int64) {
	for {
		old := target.Load()
		if v <= old {
			return
		}
		if target.CompareAndSwap(old, v) {
			return
		}
	}
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

// ========== 更真实的业务场景 RPC ==========

// 业务延迟（模拟数据库查询、缓存访问等）
// 通过环境变量 BENCH_BIZ_DELAY_US 控制，单位微秒，默认 0
var bizDelayUs = func() int64 {
	if v := os.Getenv("BENCH_BIZ_DELAY_US"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return 0
}()

// RpcGetUserData - 模拟获取用户数据（如从 Redis/MySQL）
func (s *ConcurrencyTest1Module) RpcGetUserData(req *msg.UserDataReq) *msg.UserDataResp {
	// 模拟业务处理延迟
	if bizDelayUs > 0 {
		time.Sleep(time.Duration(bizDelayUs) * time.Microsecond)
	}

	// 模拟返回数据
	return &msg.UserDataResp{
		Code:          0,
		Msg:           "success",
		UserId:        req.UserId,
		Nickname:      "Player_" + strconv.FormatInt(req.UserId, 10),
		Level:         int32(req.UserId % 100),
		Exp:           req.UserId * 1000,
		Gold:          req.UserId * 100,
		Diamond:       req.UserId * 10,
		Items:         []int32{1001, 1002, 1003, 2001, 2002, 3001},
		LastLoginTime: time.Now().Unix() - 3600,
		ServerTime:    time.Now().Unix(),
	}
}

// RpcBattle - 模拟战斗计算
func (s *ConcurrencyTest1Module) RpcBattle(req *msg.BattleReq) *msg.BattleResp {
	// 模拟业务处理延迟
	if bizDelayUs > 0 {
		time.Sleep(time.Duration(bizDelayUs) * time.Microsecond)
	}

	// 模拟战斗计算
	damage := int32(req.SkillId * 10)
	if len(req.Params) > 0 {
		damage += req.Params[0]
	}
	isCritical := req.PlayerId%7 == 0 // 简单模拟暴击

	return &msg.BattleResp{
		Code:       0,
		Damage:     damage,
		IsCritical: isCritical,
		RemainHp:   1000 - damage,
		Timestamp:  time.Now().UnixNano(),
	}
}

type ConcurrencyTest1 struct {
	core.Service
}

func (s *ConcurrencyTest1) OnInit() error {
	_, _ = s.AddModule(&ConcurrencyTest1Module{})
	return nil
}

func (s *ConcurrencyTest1) EmptyFun() {
	if diag.Enabled() {
		log.SysLogger.Debugf("EmptyFun")
	}
}

func (s *ConcurrencyTest1) RpcEmptyFun() {

}

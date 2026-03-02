// Package main
// @Title  title
// @Description  desc
// @Author  yr  2024/12/4
// @Update  yr  2024/12/4
package main

import (
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/node"
	"github.com/njtc406/emberengine/engine/pkg/services"
	"github.com/njtc406/emberengine/engine/pkg/sysService/pprofservice"
	"github.com/njtc406/emberengine/example/comm"
)

func init() {
	pprofservice.RegisterPprofService()

	services.SetService("ConcurrencyTest", func() inf.IService {
		return &comm.ConcurrencyTest{}
	})
}

func main() {
	//runtime.GOMAXPROCS(16) // 匹配CPU核心数
	//runtime.SetMutexProfileFraction(1)

	// GC 调优：减少 GC 频率以降低 STW 抖动
	// GOGC=400 表示 heap 增长到 4 倍时才触发 GC（默认 100 = 2 倍）
	// GOMEMLIMIT 可限制最大内存，防止 OOM
	if _, ok := os.LookupEnv("GOGC"); !ok {
		debug.SetGCPercent(400) // 减少 GC 频率
	}
	// 可选：设置内存上限防止 OOM（根据机器内存调整）
	// debug.SetMemoryLimit(2 * 1024 * 1024 * 1024) // 2GB
	// REMOTE_HOST 由 example/configs/**/.env 或启动前环境变量提供。
	// 不要在这里设置默认值，否则会覆盖 .env（.env 在 node.Start 内部才加载）。
	// 默认关闭 per-op 耗时采集，避免 time.Now/time.Since + durations 写入污染 QPS。
	// 如需分位数延迟统计，请显式设置 BENCH_RECORD_DURATIONS=1。
	if _, ok := os.LookupEnv("BENCH_RECORD_DURATIONS"); !ok {
		_ = os.Setenv("BENCH_RECORD_DURATIONS", "0")
	}
	// 默认关闭 last success 时间追踪（每次成功都会 time.Now().UnixNano()+atomic），纯 QPS 更准确。
	// 如需 success time/tail wait，请显式设置 BENCH_TRACK_SUCCESS_TS=1。
	if _, ok := os.LookupEnv("BENCH_TRACK_SUCCESS_TS"); !ok {
		_ = os.Setenv("BENCH_TRACK_SUCCESS_TS", "0")
	}
	n, err := node.New().Start(node.WithConfPath("./example/configs/node_concurrency"))
	if err != nil {
		panic(err)
	}
	exitCh := make(chan os.Signal, 1)
	signal.Notify(exitCh, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGINT)
	<-exitCh
	fmt.Println("exit signal received")
	n.Stop()
}

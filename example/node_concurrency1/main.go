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
	"syscall"

	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/node"
	"github.com/njtc406/emberengine/engine/pkg/services"
	"github.com/njtc406/emberengine/engine/pkg/sysService/pprofservice"
	"github.com/njtc406/emberengine/example/comm"
)

func init() {
	pprofservice.RegisterPprofService()

	services.SetService("ConcurrencyTest1", func() inf.IService {
		return &comm.ConcurrencyTest1{}
	})
}

func main() {
	//runtime.GOMAXPROCS(16) // 匹配CPU核心数
	//runtime.SetMutexProfileFraction(1)

	if _, ok := os.LookupEnv("EMBER_LOG_STDOUT"); !ok {
		_ = os.Setenv("EMBER_LOG_STDOUT", "0")
	}

	if _, ok := os.LookupEnv("BENCH_BIZ_DELAY_US"); !ok {
		_ = os.Setenv("BENCH_BIZ_DELAY_US", "100")
	}

	// REMOTE_HOST 由 example/configs/**/.env 或启动前环境变量提供。
	// 不要在这里设置默认值，否则会覆盖 .env（.env 在 node.Start 内部才加载）。
	n, err := node.New().Start(node.WithConfPath("./example/configs/node_concurrency1"))
	if err != nil {
		panic(err)
	}
	exitCh := make(chan os.Signal, 1)
	signal.Notify(exitCh, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGINT)
	<-exitCh
	fmt.Println("exit signal received")
	n.Stop()
}

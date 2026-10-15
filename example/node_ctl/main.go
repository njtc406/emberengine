// Package main
// @Title  title
// @Description  控制节点,用来测试全局事件等等
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
	"github.com/njtc406/emberengine/example/comm"
)

func init() {
	services.SetService("MasterSlaverTest", func() inf.IService {
		return &comm.MasterSlaverTest{}
	})
}

func main() {
	n, err := node.New().Start(node.WithConfPath("./example/configs/node_slave1"))
	if err != nil {
		panic(err)
	}
	exitCh := make(chan os.Signal, 1)
	signal.Notify(exitCh, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGINT)
	<-exitCh
	fmt.Println("exit signal received")
	n.Stop()
}

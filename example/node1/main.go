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
	"github.com/njtc406/emberengine/example/comm"
)

func init() {
	services.SetService("Service2", func() inf.IService {
		return &comm.Service2{}
	})
	services.SetService("Service1", func() inf.IService {
		return &comm.Service1{}
	})
}

var exitCh = make(chan os.Signal, 1)

func init() {
	// 注册退出信号
	signal.Notify(exitCh, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGINT)
}

func main() {
	n, err := node.New().Start(node.WithConfPath("./example/configs/node1"))
	if err != nil {
		panic(err)
	}
	select {
	case <-exitCh:
		fmt.Println("exit signal received")
	}
	n.Stop()
}

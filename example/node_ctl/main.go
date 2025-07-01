// Package main
// @Title  title
// @Description  控制节点,用来测试全局事件等等
// @Author  yr  2024/12/4
// @Update  yr  2024/12/4
package main

import (
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/node"
	"github.com/njtc406/emberengine/engine/pkg/services"
	"github.com/njtc406/emberengine/example/comm"
)

func init() {
	services.SetService("Service111", func() inf.IService {
		return &comm.Service111{}
	})
}

func main() {
	node.Start(node.WithConfPath("./example/configs/node_slave1"))
}

// Package main
// @Title  title
// @Description  desc
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
	services.SetService("ConcurrencyTest1", func() inf.IService {
		return &comm.ConcurrencyTest1{}
	})
}

func main() {
	//runtime.GOMAXPROCS(16) // 匹配CPU核心数
	//runtime.SetMutexProfileFraction(1)
	node.Start(node.WithConfPath("./example/configs/node_concurrency1"))
}

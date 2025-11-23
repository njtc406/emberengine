// Package main
// @Title  title
// @Description  desc
// @Author  yr  2024/12/4
// @Update  yr  2024/12/4
package main

import (
	comm2 "github.com/njtc406/emberengine/engine/example/comm"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/node"
	"github.com/njtc406/emberengine/engine/pkg/services"
)

func init() {
	services.SetService("Service2", func() inf.IService {
		return &comm2.Service2{}
	})
	services.SetService("Service1", func() inf.IService {
		return &comm2.Service1{}
	})
}

func main() {
	node.Start(node.WithConfPath("./example/configs/node1"))
}

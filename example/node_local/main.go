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
	_ "github.com/njtc406/emberengine/engine/pkg/sysService/pprofservice"
	"github.com/njtc406/emberengine/example/comm"
)

func init() {
	services.SetService("Service3", func() inf.IService {
		return &comm.Service3{}
	})
	services.SetService("Service2", func() inf.IService {
		return &comm.Service2{}
	})
	services.SetService("Service1", func() inf.IService {
		return &comm.Service1{}
	})
}

var version = "1.0"

func main() {
	node.Start(
		node.WithConfPath("./example/configs/node_local"),
		node.WithVersion(version),
	)
}

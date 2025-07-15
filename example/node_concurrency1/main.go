// Package main
// @Title  title
// @Description  desc
// @Author  yr  2024/12/4
// @Update  yr  2024/12/4
package main

import (
	"github.com/njtc406/emberengine/engine/pkg/core"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/node"
	"github.com/njtc406/emberengine/engine/pkg/services"
	"github.com/njtc406/emberengine/example/msg"
	"runtime"
)

type Service2TestModule struct {
	core.Module
}

func (s *Service2TestModule) RpcSum(req *msg.Msg_Test_Req) *msg.Msg_Test_Resp {
	//log.SysLogger.Debugf(">>>>>>>>>>> call %s func RpcSum, a:%d, b:%d", s.GetModuleName(), a, b)
	return &msg.Msg_Test_Resp{Ret: req.A + req.B}
}

func (s *Service2TestModule) ApiSum(a, b int) int {
	//log.SysLogger.Debugf(">>>>>>>>>>> call %s func ApiSum, a:%d, b:%d", s.GetModuleName(), a, b)
	return a + b
}

type Service2 struct {
	core.Service
}

func (s *Service2) OnInit() error {
	_, _ = s.AddModule(&Service2TestModule{})
	return nil
}

func (s *Service2) EmptyFun() {

}

func (s *Service2) RpcEmptyFun() {

}

func init() {
	services.SetService("Service2", func() inf.IService {
		return &Service2{}
	})
}

func main() {
	runtime.GOMAXPROCS(16) // 匹配CPU核心数
	runtime.SetMutexProfileFraction(1)
	node.Start(node.WithConfPath("./example/configs/node_concurrency1"))
}

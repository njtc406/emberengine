// Package main
// @Title  title
// @Description  desc
// @Author  yr  2024/12/4
// @Update  yr  2024/12/4
package main

import (
	"fmt"
	"github.com/njtc406/emberengine/engine/pkg/core"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/node"
	"github.com/njtc406/emberengine/engine/pkg/services"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/njtc406/emberengine/example/msg"
)

type Service111 struct {
	core.Service
}

func (s *Service111) OnInit() error {
	return nil
}

func (s *Service111) OnStart() error {
	return nil
}

func (s *Service111) OnRelease() {

}

func (s *Service111) RPCTest2() {
	//time.Sleep(time.Second * 3) // 模拟耗时操作
	log.SysLogger.Debugf("call %s func RPCTest2", s.GetName())
}

func (s *Service111) RPCSum(req *msg.Msg_Test_Req) *msg.Msg_Test_Resp {
	//time.Sleep(time.Second * 2)
	return &msg.Msg_Test_Resp{
		Ret: req.A + req.B,
	}
}

func (s *Service111) RPCTestWithError(req *msg.Msg_Test_Req) (*msg.Msg_Test_Resp, error) {
	return nil, fmt.Errorf("rpc test")
}

func init() {
	services.SetService("Service111", func() inf.IService {
		return &Service111{}
	})
}

var version = "1.0"

func main() {
	node.Start(version, "./example/configs/node_slave")
}

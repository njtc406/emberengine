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

	a int
}

func (s *Service111) OnInit() error {
	return nil
}

func (s *Service111) OnStart() error {
	return nil
}

func (s *Service111) OnStarted() error {
	if s.GetPid().GetIsMaster() {
		// 如果是主服务,则注册监听从服务的事件
		// TODO 感觉这里不太对的样子,但是主从同步时应该是自定义的东西,所以好像又是对的
		s.GetEventProcessor().RegSlaverEventReceiverFunc(s.GetEventHandler(), s.syncMasterDataToSlaver)
	} else {
		// 是从服务,监听主服务事件
		s.GetEventProcessor().RegMasterEventReceiverFunc(s.GetEventHandler(), s.receiveMasterEvent)
	}
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

func (s *Service111) syncMasterDataToSlaver(e inf.IEvent) {

}

func (s *Service111) receiveMasterEvent(e inf.IEvent) {

}

func init() {
	services.SetService("Service111", func() inf.IService {
		return &Service111{}
	})
}

var version = "1.0"

func main() {
	node.Start(version, "./example/configs/node_master")
}

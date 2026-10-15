// Package comm
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/18 0018 20:04
// 最后更新:  yr  2025/7/18 0018 20:04
package comm

import (
	"context"

	"github.com/njtc406/emberengine/engine/pkg/core"
	"github.com/njtc406/emberengine/example/msg"
)

type Service3 struct {
	core.Service
}

func (s *Service3) OnInit() error {
	return nil
}

func (s *Service3) OnStart() error {
	return nil
}

func (s *Service3) OnRelease() {

}

func (s *Service3) RPCTest2(ctx context.Context) {
	//time.Sleep(time.Second * 4) // 模拟耗时操作
	s.WithContext(ctx).Debug("call func RPCTest2")
}

func (s *Service3) RPCSum(req *msg.Msg_Test_Req) *msg.Msg_Test_Resp {
	//time.Sleep(time.Second * 2)
	s.Debug("call func RPCSum")
	return &msg.Msg_Test_Resp{
		Ret: req.A + req.B,
	}
}

func (s *Service3) RpcTestWithError(_ *msg.Msg_Test_Req) (*msg.Msg_Test_Resp, error) {
	s.Debug("call func RpcTestWithError")
	return nil, nil
	//return nil, fmt.Errorf("rpc test")
}

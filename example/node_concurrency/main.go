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
)

const (
	ServiceName1 = "Service1"
	ServiceName2 = "Service2"
	ServiceName3 = "Service3"
)

type Service1 struct {
	core.Service

	autoCallTimerId uint64
}

func (s *Service1) OnInit() error {

	return nil
}

func (s *Service1) OnStart() error {
	// 测试在onstart阶段call其他服务
	//s.callTest()
	return nil
}

func (s *Service1) OnRelease() {

}

type Service2TestModule struct {
	core.Module
}

func (s *Service2TestModule) APISum(a, b int) int {
	//log.SysLogger.Debugf(">>>>>>>>>>> call %s func APISum, a:%d, b:%d", s.GetModuleName(), a, b)
	return a + b
}

type Service2 struct {
	core.Service
}

func (s *Service2) OnInit() error {
	_, _ = s.AddModule(&Service2TestModule{})
	return nil
}

func (s *Service2) OnStart() error {
	//s.AsyncDo(func() bool {
	//	time.Sleep(time.Second)
	//	// 创建service1
	//	svc1 := &Service1{}
	//	svc1.OnSetup(svc1)
	//	svc1.Init(svc1, nil, nil)
	//	svc1.OnInit()
	//	if err := svc1.Start(); err != nil {
	//		log.SysLogger.Errorf("start Service1 failed, err:%v", err)
	//	}
	//	return true
	//}, nil)
	return nil
}

func (s *Service2) OnRelease() {

}

func (s *Service2) EmptyFun() {

}

func init() {
	services.SetService("Service2", func() inf.IService {
		return &Service2{}
	})
	services.SetService("Service1", func() inf.IService {
		return &Service1{}
	})
}

func main() {
	node.Start(node.WithConfPath("./example/configs/node1"))
}

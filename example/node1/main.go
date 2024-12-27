// Package main
// @Title  title
// @Description  desc
// @Author  yr  2024/12/4
// @Update  yr  2024/12/4
package main

import (
	"github.com/njtc406/emberengine/engine/core"
	"github.com/njtc406/emberengine/engine/core/node"
	"github.com/njtc406/emberengine/engine/inf"
	"github.com/njtc406/emberengine/engine/services"
	"github.com/njtc406/emberengine/engine/utils/log"
	"github.com/njtc406/emberengine/engine/utils/timer"
	"time"
)

type Service1 struct {
	core.Service

	autoCallTimerId uint64
}

func (s *Service1) OnInit() error {
	// api test demo

	//s.AfterFunc(time.Second, func(timer timer.ITimer) {
	//	//startTime := timelib.GetTime()
	//	// 调用Service2.APITest2
	//	if err := s.SelectSameServer("", "Service2").Call("APITest2", nil, nil); err != nil {
	//		log.SysLogger.Errorf("call Service2.APITest2 failed, err:%v", err)
	//	}
	//	if err := s.SelectSameServer("", "Service2").Send("APITest2", nil); err != nil {
	//		log.SysLogger.Errorf("call Service2.APITest2 failed, err:%v", err)
	//	}
	//	//log.SysLogger.Debugf("call Service2.APITest2 cost:%d", timelib.Since(startTime).Microseconds())
	//})
	//s.AfterFunc(time.Second*2, func(iTimer timer.ITimer) {
	//	// 调用Service2.APITest2 带返回参数
	//	var out int
	//	if err := s.SelectSameServer("", "Service2").Call("APISum", []interface{}{1, 2}, &out); err != nil {
	//		log.SysLogger.Errorf("call Service2.APISum failed, err:%v", err)
	//	}
	//
	//	log.SysLogger.Debugf("call Service2.APISum out:%d", out)
	//})
	//
	//s.AfterFunc(time.Second*3, func(iTimer timer.ITimer) {
	//	// 调用Service2.APITest2 不同类型入参
	//	if err := s.SelectSameServer("", "Service2").Call("APIPrintParams", []interface{}{1, "2"}, nil); err != nil {
	//		log.SysLogger.Errorf("call Service2.APIPrintParams failed, err:%v", err)
	//	}
	//
	//	// 模拟有入参,但是不传
	//	if err := s.SelectSameServer("", "Service2").Call("APIPrintParams", nil, nil); err != nil {
	//		log.SysLogger.Errorf("call Service2.APIPrintParams failed, err:%v", err)
	//	}
	//})
	//s.AfterFunc(time.Second*4, func(iTimer timer.ITimer) {
	//	// 调用Service2.APITest2 可变参数
	//	type abc struct{ a, b int }
	//	if err := s.SelectSameServer("", "Service2").Call("APIPrintIndefiniteParams", []interface{}{1, "2", abc{1, 2}, "ddddd"}, nil); err != nil {
	//		log.SysLogger.Errorf("call Service2.APIPrintIndefiniteParams failed, err:%v", err)
	//	}
	//})
	//s.AfterFunc(time.Second*5, func(iTimer timer.ITimer) {
	//	// 调用Service2.APITest2 多返回值
	//	var out int
	//	var out2 string
	//	if err := s.SelectSameServer("", "Service2").Call("APIMultiRet", nil, []interface{}{&out, &out2}); err != nil {
	//		log.SysLogger.Errorf("call Service2.APIMultiRet failed, err:%v", err)
	//	}
	//	log.SysLogger.Debugf("call Service2.APIMultiRet out:%d, out2:%s", out, out2)
	//})
	//
	//s.AfterFunc(time.Second*6, func(iTimer timer.ITimer) {
	//	// 调用Service2.APICallback 两个service相互调用(请注意,如果是相互调用,只能是非阻塞类型的调用!!!不然会发生死锁!!!)
	//	if err := s.SelectSameServer("", "Service2").Send("APICallback", nil); err != nil {
	//		log.SysLogger.Errorf("call Service2.APICallback failed, err:%v", err)
	//	}
	//})

	// rpc test demo

	//s.AfterFunc(time.Second*2, func(iTimer timer.ITimer) {
	//	if err := s.SelectSameServer("", "Service3").Call("RPCTest2", nil, nil); err != nil {
	//		log.SysLogger.Errorf("call Service3.RPCTest2 failed, err:%v", err)
	//	}
	//	if err := s.SelectSameServer("", "Service3").CallWithTimeout("RPCTest2", time.Second*5, nil, nil); err != nil {
	//		log.SysLogger.Errorf("call Service3.RPCTest2 failed, err:%v", err)
	//	}
	//	if err := s.SelectSameServer("", "Service3").Send("RPCTest2", nil); err != nil {
	//		log.SysLogger.Errorf("call Service3.RPCTest2 failed, err:%v", err)
	//	}
	//})
	//s.AfterFunc(time.Second*2, func(iTimer timer.ITimer) {
	//	out := &msg.Msg_Test_Resp{}
	//	if err := s.SelectSameServer("", "Service3").Call("RPCSum", &msg.Msg_Test_Req{A: 1, B: 2}, out); err != nil {
	//		log.SysLogger.Errorf("call Service3.RPCSum failed, err:%v", err)
	//	}
	//	if err := s.SelectSameServer("", "Service3").Send("RPCSum", &msg.Msg_Test_Req{A: 1, B: 3}); err != nil {
	//		log.SysLogger.Errorf("send Service3.RPCSum failed, err:%v", err)
	//	}
	//	if _, err := s.SelectSameServer("", "Service3").AsyncCall("RPCSum", time.Second, &msg.Msg_Test_Req{A: 1, B: 2}, func(data interface{}, err error) {
	//		if err != nil {
	//			log.SysLogger.Errorf("AsyncCall Service3.RPCSum response failed, err:%v", err)
	//			return
	//		}
	//		resp := data.(*msg.Msg_Test_Resp)
	//		log.SysLogger.Debugf("AsyncCall Service3.RPCSum out:%d", resp.Ret)
	//	}); err != nil {
	//		log.SysLogger.Errorf("AsyncCall Service3.RPCSum failed, err:%v", err)
	//	}
	//
	//	// 测试调用对象返回,但是不接收
	//	if err := s.SelectSameServer("", "Service3").Call("RPCTestWithError", &msg.Msg_Test_Req{A: 1, B: 2}, nil); err != nil {
	//		log.SysLogger.Errorf("call Service3.RPCPrintParams failed, err:%v", err)
	//	}
	//
	//	// 测试调用对象有参数,但是使用nil的情形
	//	if err := s.SelectSameServer("", "Service3").CallWithTimeout("RpcTestWithError", time.Second*3, nil, nil); err != nil {
	//		log.SysLogger.Errorf("call Service3.RPCPrintParams failed, err:%v", err)
	//	}
	//})

	// cast test
	s.AfterFunc(time.Second*1, func(iTimer timer.ITimer) {
		s.SelectSameServerByServiceType("test", "Service3").Cast("RPCTest2", nil)
	})

	// other test
	//s.AfterFunc(time.Second*7, func(iTimer timer.ITimer) {
	//	// TODO 测试各种类型的筛选器
	//})

	return nil
}

func (s *Service1) OnStart() error {
	// 测试在onstart阶段call其他服务
	//s.callTest()
	return nil
}

func (s *Service1) OnRelease() {

}

func (s *Service1) APITest1() {
	log.SysLogger.Debugf("call %s func APITest1", s.GetName())
}

//func (s *Service1) callTest() {
//	var out int
//	if err := s.Select(1, "", "Service2").Call("APISum", []interface{}{1, 2}, &out); err != nil {
//		log.SysLogger.Errorf("call Service2.APISum failed, err:%v", err)
//	}
//
//	log.SysLogger.Debugf("call Service2.APISum out:%d", out)
//}

type Service2 struct {
	core.Service
}

func (s *Service2) OnInit() error {
	s.OpenConcurrentByNumCPU(1)
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

func (s *Service2) APITest2() {
	//time.Sleep(time.Second * 5) // 模拟耗时操作
	log.SysLogger.Debugf("call %s func APITest2", s.GetName())
}

func (s *Service2) APISum(a, b int) int {
	return a + b
}

func (s *Service2) APIPrintParams(a int, b string) error {
	log.SysLogger.Debugf("call %s func APIPrintParams, a:%d, b:%s", s.GetName(), a, b)
	return nil
	//return fmt.Errorf("test")
}

func (s *Service2) APIPrintIndefiniteParams(args ...any) error {
	for _, arg := range args {
		log.SysLogger.Debugf("call %s func APIPrintIndefiniteParams, arg:%+v", s.GetName(), arg)
	}
	return nil
	//return fmt.Errorf("test")
}

func (s *Service2) APIMultiRet() (int, string, error) {
	return 1, "2", nil
}

func (s *Service2) APICallback() {
	log.SysLogger.Debugf("call %s func APICallback", s.GetName())
	if err := s.SelectSameServer("", "Service1").Send("APITest1", nil); err != nil {
		log.SysLogger.Errorf("call Service1.APITest1 failed, err:%v", err)
	}
}

func init() {
	services.SetService("Service2", func() inf.IService {
		return &Service2{}
	})
	services.SetService("Service1", func() inf.IService {
		return &Service1{}
	})
}

var version = "1.0"

func main() {
	node.Start(version, "./example/configs/node1")
}

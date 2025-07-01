// Package main
// @Title  title
// @Description  desc
// @Author  yr  2024/12/4
// @Update  yr  2024/12/4
package main

import (
	"context"
	"fmt"
	"github.com/njtc406/emberengine/engine/pkg/core"
	"github.com/njtc406/emberengine/engine/pkg/core/rpc"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/node"
	"github.com/njtc406/emberengine/engine/pkg/services"
	"github.com/njtc406/emberengine/engine/pkg/utils/emberctx"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
	"github.com/njtc406/emberengine/example/msg"
	"time"
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
	ctx := context.Background()
	ctx = emberctx.AddHeader(ctx, "traceId", "123")
	// method test demo
	s.AfterFunc(time.Second*2, "method test demo", func(timer *timingwheel.Timer, args ...interface{}) {
		//startTime := timelib.GetTime()
		// 调用Service2.APITest2
		ctxWithTimeout, cancel := context.WithTimeout(ctx, time.Second*10)
		defer cancel()
		if err := s.Select(rpc.WithServiceName(ServiceName2)).Call(ctxWithTimeout, "APITest2", nil, nil); err != nil {
			log.SysLogger.Errorf("call Service2.APITest2 failed, err:%v", err)
		}
		if err := s.Select(rpc.WithServiceName(ServiceName2)).Call(ctx, "APITest2", nil, nil); err != nil {
			log.SysLogger.Errorf("call Service2.APITest2 failed, err:%v", err)
		}
		if err := s.Select(rpc.WithServiceName(ServiceName2)).Send(ctx, "APITest2", nil); err != nil {
			log.SysLogger.Errorf("loop call Service2.APITest2 failed, err:%v", err)
		}

		// 循环call
		for i := 0; i < 10; i++ {
			if err := s.Select(rpc.WithServiceName(ServiceName2)).Call(ctx, "APITest2", nil, nil); err != nil {
				log.SysLogger.Errorf("call Service2.APITest2 failed, err:%v", err)
			}
		}

		//log.SysLogger.Debugf("call Service2.APITest2 cost:%d", timelib.Since(startTime).Microseconds())
	})
	s.AfterFunc(time.Second*2, "method test demo1", func(timer *timingwheel.Timer, args ...interface{}) {
		// 调用Service2.APITest2 带返回参数
		var out int
		if err := s.Select(rpc.WithServiceName(ServiceName2)).Call(ctx, "APISum", []interface{}{1, 2}, &out); err != nil {
			log.SysLogger.Errorf("call Service2.APISum failed, err:%v", err)
		}

		log.SysLogger.Debugf("call Service2.APISum out:%d", out)
	})

	s.AfterFunc(time.Second*3, "method test demo2", func(timer *timingwheel.Timer, args ...interface{}) {
		// 调用Service2.APITest2 不同类型入参
		if err := s.Select(rpc.WithServiceName(ServiceName2)).Call(ctx, "APIPrintParams", []interface{}{1, "2"}, nil); err != nil {
			log.SysLogger.Errorf("call Service2.APIPrintParams failed, err:%v", err)
		}

		// 模拟有入参,但是不传
		if err := s.Select(rpc.WithServiceName(ServiceName2)).Call(ctx, "APIPrintParams", nil, nil); err != nil {
			log.SysLogger.Errorf("call Service2.APIPrintParams failed, err:%v", err)
		}
	})
	s.AfterFunc(time.Second*4, "method test demo3", func(timer *timingwheel.Timer, args ...interface{}) {
		// 调用Service2.APITest2 可变参数
		type abc struct{ a, b int }
		if err := s.Select(rpc.WithServiceName(ServiceName2)).Call(ctx, "APIPrintIndefiniteParams", []interface{}{1, "2", abc{1, 2}, "ddddd"}, nil); err != nil {
			log.SysLogger.Errorf("call Service2.APIPrintIndefiniteParams failed, err:%v", err)
		}
	})
	s.AfterFunc(time.Second*5, "method test demo4", func(timer *timingwheel.Timer, args ...interface{}) {
		// 调用Service2.APITest2 多返回值
		var out int
		var out2 string
		if err := s.Select(rpc.WithServiceName(ServiceName2)).Call(ctx, "APIMultiRet", nil, []interface{}{&out, &out2}); err != nil {
			log.SysLogger.Errorf("call Service2.APIMultiRet failed, err:%v", err)
		}
		log.SysLogger.Debugf("call Service2.APIMultiRet out:%d, out2:%s", out, out2)
	})

	s.AfterFunc(time.Second*6, "method test demo5", func(timer *timingwheel.Timer, args ...interface{}) {
		// 调用Service2.APICallback 两个service相互调用(请注意,如果是相互调用,只能是非阻塞类型的调用!!!不然会发生死锁!!!)
		if err := s.Select(rpc.WithServiceName(ServiceName2)).Send(ctx, "APICallback", nil); err != nil {
			log.SysLogger.Errorf("call Service2.APICallback failed, err:%v", err)
		}
	})

	//rpc test demo

	s.AfterFunc(time.Second*2, "rpc test demo", func(timer *timingwheel.Timer, args ...interface{}) {
		if err := s.Select(rpc.WithServiceName(ServiceName3), rpc.WithServiceId("1")).Call(ctx, "RPCTest2", nil, nil); err != nil {
			log.SysLogger.Errorf("call Service3.RPCTest2 failed, err:%v", err)
		}
		ctxWithTimeout, cancel := context.WithTimeout(ctx, time.Second*5)
		defer cancel()
		if err := s.Select(rpc.WithServiceName(ServiceName3), rpc.WithServiceId("2")).Call(ctxWithTimeout, "RPCTest2", nil, nil); err != nil {
			log.SysLogger.Errorf("call Service3.RPCTest2 failed, err:%v", err)
		}
		if err := s.Select(rpc.WithServiceName(ServiceName3), rpc.WithServiceId("1")).Send(ctx, "RPCTest2", nil); err != nil {
			log.SysLogger.Errorf("call Service3.RPCTest2 failed, err:%v", err)
		}
	})
	s.AfterFunc(time.Second*2, "rpc test demo1", func(timer *timingwheel.Timer, args ...interface{}) {
		out := &msg.Msg_Test_Resp{}
		if err := s.Select(rpc.WithServiceName(ServiceName3), rpc.WithServiceId("1")).Call(ctx, "RPCSum", &msg.Msg_Test_Req{A: 1, B: 2}, out); err != nil {
			log.SysLogger.Errorf("call Service3.RPCSum failed, err:%v", err)
		}
		if err := s.Select(rpc.WithServiceName(ServiceName3), rpc.WithServiceId("2")).Send(ctx, "RPCSum", &msg.Msg_Test_Req{A: 1, B: 3}); err != nil {
			log.SysLogger.Errorf("send Service3.RPCSum failed, err:%v", err)
		}
		if _, err := s.Select(rpc.WithServiceName(ServiceName3), rpc.WithServiceId("1")).AsyncCall(ctx, "RPCSum", &msg.Msg_Test_Req{A: 1, B: 2}, &dto.AsyncCallParams{
			Params: []interface{}{1, 2, "a"},
		}, func(params []interface{}, data interface{}, err error) {
			if err != nil {
				log.SysLogger.Errorf("AsyncCall Service3.RPCSum response failed, err:%v", err)
				return
			}

			log.SysLogger.Debugf("AsyncCall Service3.RPCSum params:%+v", params)

			resp := data.(*msg.Msg_Test_Resp)
			log.SysLogger.Debugf("AsyncCall Service3.RPCSum out:%d", resp.Ret)
		}); err != nil {
			log.SysLogger.Errorf("AsyncCall Service3.RPCSum failed, err:%v", err)
		}

		// 测试调用对象返回错误
		if err := s.Select(rpc.WithServiceName(ServiceName3), rpc.WithServiceId("1")).Call(ctx, "RPCTestWithError", &msg.Msg_Test_Req{A: 1, B: 2}, nil); err != nil {
			log.SysLogger.Errorf("call Service3.RPCTestWithError failed, err:%v", err)
		}

		// 测试调用对象有参数,但是使用nil的情形
		ctxWithTimeout, cancel := context.WithTimeout(ctx, time.Second*3)
		defer cancel()
		ctxWithTimeout = emberctx.AddHeader(ctxWithTimeout, "traceId", "222")
		if err := s.Select(rpc.WithServiceName(ServiceName3), rpc.WithServiceId("1")).Call(ctxWithTimeout, "RpcTestWithError", nil, nil); err != nil {
			log.SysLogger.Errorf("call Service3.RpcTestWithError failed, err:%v", err)
		}
	})

	//cast test
	s.AfterFunc(time.Second*2, "cast test", func(timer *timingwheel.Timer, args ...interface{}) {
		log.SysLogger.Debugf("================================>>>")
		// 1. 让node2开启多线程,然后调用10次cast
		for i := 0; i < 10; i++ {
			s.SelectByServiceType(1, "test", "Service3").Send(ctx, "RPCTest2", nil)
		}
		// 2. 让node2开启单线程,然后调用1次cast
		s.SelectSameServerByServiceType("test", "Service3").Send(ctx, "RPCTest2", nil)
	})

	// other test
	//s.AfterFunc(time.Second*7, "other test", func(timer *timingwheel.Timer, args ...interface{}) {
	//	// TODO 测试各种类型的筛选器
	//})

	// 测试各种参数情况
	s.AfterFunc(time.Second, "test2", func(timer *timingwheel.Timer, args ...interface{}) {
		var out int
		// 1. 固定参数->全部参数 (正常调用)
		if err := s.Select(rpc.WithServiceName(ServiceName2)).Call(ctx, "APIWithFixedParams", []interface{}{1, 2}, &out); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithFixedParams failed, err:%v", err)
		}
		log.SysLogger.Debugf("call Service2.APIWithFixedParams out1:%d", out)

		// 2. 固定参数->部分参数 (返回参数数量错误)
		if err := s.Select(rpc.WithServiceName(ServiceName2)).Call(ctx, "APIWithFixedParams", []interface{}{4}, &out); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithFixedParams failed, err:%v", err)
		}
		log.SysLogger.Debugf("call Service2.APIWithFixedParams out2:%d", out)

		// 3. 固定参数->没有参数 (返回参数数量错误)
		if err := s.Select(rpc.WithServiceName(ServiceName2)).Call(ctx, "APIWithFixedParams", nil, &out); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithFixedParams failed, err:%v", err)
		}
		log.SysLogger.Debugf("call Service2.APIWithFixedParams out3:%d", out)

		// 4. 固定参数->全部参数,不接收返回值 (正常调用,out依然输出3)
		if err := s.Select(rpc.WithServiceName(ServiceName2)).Call(ctx, "APIWithFixedParams", []interface{}{5, 6}, nil); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithFixedParams failed, err:%v", err)
		}
		log.SysLogger.Debugf("call Service2.APIWithFixedParams out4:%d", out)

		//5. 不固定参数->有固定参数和不定参和返回值 给出全部参数 (正常调用)
		if err := s.Select(rpc.WithServiceName(ServiceName2)).Call(ctx, "APIWithFixedAndIndefiniteParams", []interface{}{7, 8, "9", "10"}, &out); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithFixedAndIndefiniteParams failed, err:%v", err)
		}
		log.SysLogger.Debugf("call Service2.APIWithFixedAndIndefiniteParams out5:%d", out)

		// 6. 不固定参数->有固定参数和不定参和返回值 给出全部固定部分,不给变参部分 (正常调用)
		if err := s.Select(rpc.WithServiceName(ServiceName2)).Call(ctx, "APIWithFixedAndIndefiniteParams", []interface{}{11, 12}, &out); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithFixedAndIndefiniteParams failed, err:%v", err)
		}
		log.SysLogger.Debugf("call Service2.APIWithFixedAndIndefiniteParams out6:%d", out)

		// 7. 不固定参数->有固定参数和不定参和返回值 给出部分固定部分,不给变参部分 (返回参数数量错误)
		if err := s.Select(rpc.WithServiceName(ServiceName2)).Call(ctx, "APIWithFixedAndIndefiniteParams", []interface{}{13}, &out); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithFixedAndIndefiniteParams failed, err:%v", err)
		}
		log.SysLogger.Debugf("call Service2.APIWithFixedAndIndefiniteParams out7:%d", out)

		// 8.  不固定参数->有固定参数和不定参和返回值 不给任何参数 (返回参数数量错误)
		if err := s.Select(rpc.WithServiceName(ServiceName2)).Call(ctx, "APIWithFixedAndIndefiniteParams", nil, &out); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithFixedAndIndefiniteParams failed, err:%v", err)
		}
		log.SysLogger.Debugf("call Service2.APIWithFixedAndIndefiniteParams out8:%d", out)

		// 9. 不固定参数->有固定参数和不定参和返回值 给出全部参数,不接收返回值 (正常调用,out依然输出23)
		if err := s.Select(rpc.WithServiceName(ServiceName2)).Call(ctx, "APIWithFixedAndIndefiniteParams", []interface{}{14, 15, "16", "17"}, nil); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithFixedAndIndefiniteParams failed, err:%v", err)
		}
		log.SysLogger.Debugf("call Service2.APIWithFixedAndIndefiniteParams out8:%d", out)

		//9. 没有参数-> 有返回值 (正常调用)
		if err := s.Select(rpc.WithServiceName(ServiceName2)).Call(ctx, "APIWithoutParams", nil, &out); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithoutParams failed, err:%v", err)
		}
		log.SysLogger.Debugf("call Service2.APIWithoutParams out:%d", out)

		// 10. 没有参数-> 给一个参数 (返回参数数量错误)
		if err := s.Select(rpc.WithServiceName(ServiceName2)).Call(ctx, "APIWithoutParams", 1, &out); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithoutParams failed, err:%v", err)
		}
		log.SysLogger.Debugf("call Service2.APIWithoutParams out2:%d", out)

		// 11. 没有参数-> 没有返回值 (正常调用)
		if err := s.Select(rpc.WithServiceName(ServiceName2)).Call(ctx, "APIWithoutParams", nil, nil); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithoutParams failed, err:%v", err)
		}
		log.SysLogger.Debugf("call Service2.APIWithoutParams out3:%d", out)

		//12. 多返回值 -> 全部接收 (正常调用)
		var out1, out2 int
		if err := s.Select(rpc.WithServiceName(ServiceName2)).Call(ctx, "APIWithMultiResults", nil, []interface{}{&out1, &out2}); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithMultiResults failed, err:%v", err)
		}
		// 输出 1, 2
		log.SysLogger.Debugf("call Service2.APIWithMultiResults 1 out1:%d, out2:%d", out1, out2)

		// 13. 多返回值 -> 部分接收 (正常调用)
		if err := s.Select(rpc.WithServiceName(ServiceName2)).Call(ctx, "APIWithMultiResults", nil, []interface{}{&out2}); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithMultiResults failed, err:%v", err)
		}
		// 输出 1, 1
		log.SysLogger.Debugf("call Service2.APIWithMultiResults 2 out1:%d, out2:%d", out1, out2)

		// 14. 多返回值 -> 全都不接收 (正常调用)
		if err := s.Select(rpc.WithServiceName(ServiceName2)).Call(ctx, "APIWithMultiResults", nil, nil); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithMultiResults failed, err:%v", err)
		}
		// 输出 1, 1
		log.SysLogger.Debugf("call Service2.APIWithMultiResults 3 out1:%d, out2:%d", out1, out2)

		// 15. 多返回值 -> 使用错误类型接收 (报错)
		if err := s.Select(rpc.WithServiceName(ServiceName2)).Call(ctx, "APIWithMultiResults", nil, &out1); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithMultiResults failed, err:%v", err)
		}
		// 输出 1, 1
		log.SysLogger.Debugf("call Service2.APIWithMultiResults 4 out1:%d, out2:%d", out1, out2)
	})

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
//	if err := s.Select(rpc.WithServiceName(ServiceName2)).Call("APISum", []interface{}{1, 2}, &out); err != nil {
//		log.SysLogger.Errorf("call Service2.APISum failed, err:%v", err)
//	}
//
//	log.SysLogger.Debugf("call Service2.APISum out:%d", out)
//}

type Service2TestModule struct {
	core.Module
}

func (s *Service2TestModule) APISum(a, b int) int {
	log.SysLogger.Debugf(">>>>>>>>>>> call %s func APISum, a:%d, b:%d", s.GetModuleName(), a, b)
	return a + b
}

type Service2 struct {
	core.Service
}

func (s *Service2) OnInit() error {
	//s.OpenConcurrentByNumCPU(1)
	s.AddModule(&Service2TestModule{})
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

//func (s *Service2) APISum(a, b int) int {
//	return a + b
//}

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
	if err := s.Select(rpc.WithServiceName(ServiceName1)).Send(context.Background(), "APITest1", nil); err != nil {
		log.SysLogger.Errorf("call Service1.APITest1 failed, err:%v", err)
	}
}

func (s *Service2) APIWithFixedParams(a, b int) int {
	return a + b
}

func (s *Service2) APIWithFixedAndIndefiniteParams(a, b int, args ...string) (int, error) {
	log.SysLogger.Debugf("call %s func APIWithFixedAndIndefiniteParams, a:%d, b:%d args:%+v", s.GetName(), a, b, args)
	return a + b, nil
}

func (s *Service2) APIWithMultiResults() (int, int, error) {
	return 1, 2, nil
	return 1, 2, fmt.Errorf("test")
}

func (s *Service2) APIWithoutParams() int {
	return 1
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
	node.Start(
		node.WithConfPath("./example/configs/node1"),
		node.WithVersion(version),
	)
}

// Package comm
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/18 0018 20:01
// 最后更新:  yr  2025/7/18 0018 20:01
package comm

import (
	"context"
	"github.com/njtc406/emberengine/engine/pkg/core"
	"github.com/njtc406/emberengine/engine/pkg/core/rpc"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
	"github.com/njtc406/emberengine/engine/pkg/utils/xcontext"
	"github.com/njtc406/emberengine/example/msg"
	"time"
)

const (
	ServiceNameTest1 = "Service1"
	ServiceNameTest2 = "Service2"
	ServiceNameTest3 = "Service3"
)

type Service1 struct {
	core.Service

	autoCallTimerId uint64
}

func (s *Service1) OnInit() error {

	var ctx context.Context

	// method test demo
	s.AfterFunc(time.Second, "method test demo", func(timer *timingwheel.Timer, args ...interface{}) {
		//startTime := timelib.GetTime()
		// 调用Service2.APITest2
		ctxWithTimeout, cancel := context.WithTimeout(xcontext.New(nil), time.Second*10)
		defer cancel()
		if err := s.Select(rpc.WithServiceName(ServiceNameTest2)).Call(ctxWithTimeout, "APITest2", nil, nil); err != nil {
			s.GetLogger().Errorf("call Service2.APITest2 failed, err:%v", err)
		}
		s.GetLogger().Debugf("==========================================1111")
		if err := s.Select(rpc.WithServiceName(ServiceNameTest2)).Call(ctx, "APITest2", nil, nil); err != nil {
			log.SysLogger.Errorf("call Service2.APITest2 failed, err:%v", err)
		}
		s.GetLogger().Debugf("==========================================2222")
		if err := s.Select(rpc.WithServiceName(ServiceNameTest2)).Send(ctx, "APITest2", nil); err != nil {
			log.SysLogger.Errorf("loop call Service2.APITest2 failed, err:%v", err)
		}
		s.GetLogger().Debugf("==========================================33333")

		// 循环call
		for i := 0; i < 10; i++ {
			if err := s.Select(rpc.WithServiceName(ServiceNameTest2)).Call(ctx, "APITest2", nil, nil); err != nil {
				log.SysLogger.Errorf("call Service2.APITest2 failed, err:%v", err)
			}
		}
		s.GetLogger().Debugf("==========================================4444")
		//log.SysLogger.Debugf("call Service2.APITest2 cost:%d", timelib.Since(startTime).Microseconds())
	})
	s.AfterFunc(time.Second*2, "method test demo1", func(timer *timingwheel.Timer, args ...interface{}) {
		// 调用Service2.APITest2 带返回参数
		var out int
		if err := s.Select(rpc.WithServiceName(ServiceNameTest2)).Call(ctx, "APISum", []interface{}{1, 2}, &out); err != nil {
			log.SysLogger.Errorf("call Service2.APISum failed, err:%v", err)
		}
		s.GetLogger().Debugf("==========================================5555")
		log.SysLogger.Debugf("call Service2.APISum out:%d", out)
	})

	s.AfterFunc(time.Second*3, "method test demo2", func(timer *timingwheel.Timer, args ...interface{}) {
		// 调用Service2.APITest2 不同类型入参
		if err := s.Select(rpc.WithServiceName(ServiceNameTest2)).Call(ctx, "APIPrintParams", []interface{}{1, "2"}, nil); err != nil {
			log.SysLogger.Errorf("call Service2.APIPrintParams failed, err:%v", err)
		}
		s.GetLogger().Debugf("==========================================666666")
		// 模拟有入参,但是不传
		if err := s.Select(rpc.WithServiceName(ServiceNameTest2)).Call(ctx, "APIPrintParams", nil, nil); err != nil {
			log.SysLogger.Errorf("call Service2.APIPrintParams failed, err:%v", err)
		}
		s.GetLogger().Debugf("==========================================777777777")
	})
	s.AfterFunc(time.Second*4, "method test demo3", func(timer *timingwheel.Timer, args ...interface{}) {
		// 调用Service2.APITest2 可变参数
		type abc struct{ a, b int }
		if err := s.Select(rpc.WithServiceName(ServiceNameTest2)).Call(ctx, "APIPrintIndefiniteParams", []interface{}{1, "2", abc{1, 2}, "ddddd"}, nil); err != nil {
			log.SysLogger.Errorf("call Service2.APIPrintIndefiniteParams failed, err:%v", err)
		}
		s.GetLogger().Debugf("==========================================888888888")
	})
	s.AfterFunc(time.Second*5, "method test demo4", func(timer *timingwheel.Timer, args ...interface{}) {
		// 调用Service2.APITest2 多返回值
		var out int
		var out2 string
		if err := s.Select(rpc.WithServiceName(ServiceNameTest2)).Call(ctx, "APIMultiRet", nil, []interface{}{&out, &out2}); err != nil {
			log.SysLogger.Errorf("call Service2.APIMultiRet failed, err:%v", err)
		}
		s.GetLogger().Debugf("==========================================99999999999")
		log.SysLogger.Debugf("call Service2.APIMultiRet out:%d, out2:%s", out, out2)
	})

	s.AfterFunc(time.Second*6, "method test demo5", func(timer *timingwheel.Timer, args ...interface{}) {
		// 调用Service2.APICallback 两个service相互调用(请注意,如果是相互调用,只能是非阻塞类型的调用!!!不然会发生死锁!!!)
		if err := s.Select(rpc.WithServiceName(ServiceNameTest2)).Send(ctx, "APICallback", nil); err != nil {
			log.SysLogger.Errorf("call Service2.APICallback failed, err:%v", err)
		}

		s.GetLogger().Debugf("==========================================10")
	})

	//rpc test demo

	s.AfterFunc(time.Second*7, "rpc test demo", func(timer *timingwheel.Timer, args ...interface{}) {
		//if err := s.Select(rpc.WithServiceName(ServiceName3), rpc.WithServiceId("1")).Call(ctx, "RPCTest2", nil, nil); err != nil {
		//	log.SysLogger.Errorf("call Service3.RPCTest2 failed, err:%v", err)
		//}
		ctxWithTimeout, cancel := context.WithTimeout(xcontext.New(nil), time.Second*1000)
		defer cancel()
		if err := s.Select(rpc.WithServiceName(ServiceNameTest3), rpc.WithServiceId("2")).Call(ctxWithTimeout, "RPCTest2", nil, nil); err != nil {
			log.SysLogger.Errorf("call Service3.RPCTest2 failed, err:%v", err)
		}
		s.GetLogger().Debugf("==========================================11")
		//if err := s.Select(rpc.WithServiceName(ServiceName3), rpc.WithServiceId("1")).Send(ctx, "RPCTest2", nil); err != nil {
		//	log.SysLogger.Errorf("call Service3.RPCTest2 failed, err:%v", err)
		//}
	})
	s.AfterFunc(time.Second*8, "rpc test demo1", func(timer *timingwheel.Timer, args ...interface{}) {
		out := &msg.Msg_Test_Resp{}
		if err := s.Select(rpc.WithServiceName(ServiceNameTest3), rpc.WithServiceId("1")).Call(ctx, "RPCSum", &msg.Msg_Test_Req{A: 1, B: 2}, out); err != nil {
			log.SysLogger.Errorf("call Service3.RPCSum failed, err:%v", err)
		}
		s.GetLogger().Debugf("==========================================12")
		if err := s.Select(rpc.WithServiceName(ServiceNameTest3), rpc.WithServiceId("2")).Send(ctx, "RPCSum", &msg.Msg_Test_Req{A: 1, B: 3}); err != nil {
			log.SysLogger.Errorf("send Service3.RPCSum failed, err:%v", err)
		}
		s.GetLogger().Debugf("==========================================13")
		if _, err := s.Select(rpc.WithServiceName(ServiceNameTest3), rpc.WithServiceId("1")).AsyncCall(ctx, "RPCSum", &msg.Msg_Test_Req{A: 1, B: 2}, &dto.AsyncCallParams{
			Params: []interface{}{1, 2, "a"},
		}, func(data interface{}, err error, params ...interface{}) {
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
		s.GetLogger().Debugf("==========================================14")

		// 测试调用对象返回错误
		if err := s.Select(rpc.WithServiceName(ServiceNameTest3), rpc.WithServiceId("1")).Call(ctx, "RPCTestWithError", &msg.Msg_Test_Req{A: 1, B: 2}, nil); err != nil {
			log.SysLogger.Errorf("call Service3.RPCTestWithError failed, err:%v", err)
		}
		s.GetLogger().Debugf("==========================================15")

		// 测试调用对象有参数,但是使用nil的情形
		ctxWithTimeout, cancel := context.WithTimeout(xcontext.New(nil), time.Second*3)
		defer cancel()
		if err := s.Select(rpc.WithServiceName(ServiceNameTest3), rpc.WithServiceId("1")).Call(ctxWithTimeout, "RpcTestWithError", nil, nil); err != nil {
			log.SysLogger.Errorf("call Service3.RpcTestWithError failed, err:%v", err)
		}
		s.GetLogger().Debugf("==========================================16")
	})

	//cast test
	//s.AfterFunc(time.Second*9, "cast test", func(timer *timingwheel.Timer, args ...interface{}) {
	//	log.SysLogger.Debugf("================================>>>")
	//	// 1. 让node2开启多线程,然后调用10次cast
	//	for i := 0; i < 10; i++ {
	//		s.SelectByServiceType(1, "test", "Service3").Send(ctx, "RPCTest2", nil)
	//	}
	//	s.GetLogger().Debugf("==========================================17")
	//	// 2. 让node2开启单线程,然后调用1次cast
	//	s.SelectSameServerByServiceType("test", "Service3").Send(ctx, "RPCTest2", nil)
	//	s.GetLogger().Debugf("==========================================18")
	//})

	// other test
	//s.AfterFunc(time.Second*7, "other test", func(timer *timingwheel.Timer, args ...interface{}) {
	//	// TODO 测试各种类型的筛选器
	//})

	// 测试各种参数情况
	s.AfterFunc(time.Second*10, "test2", func(timer *timingwheel.Timer, args ...interface{}) {
		var out int
		// 1. 固定参数->全部参数 (正常调用)
		if err := s.Select(rpc.WithServiceName(ServiceNameTest2)).Call(ctx, "APIWithFixedParams", []interface{}{1, 2}, &out); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithFixedParams failed, err:%v", err)
		}
		s.GetLogger().Debugf("==========================================19")
		log.SysLogger.Debugf("call Service2.APIWithFixedParams out1:%d", out)

		// 2. 固定参数->部分参数 (返回参数数量错误)
		if err := s.Select(rpc.WithServiceName(ServiceNameTest2)).Call(ctx, "APIWithFixedParams", []interface{}{4}, &out); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithFixedParams failed, err:%v", err)
		}
		s.GetLogger().Debugf("==========================================20")
		log.SysLogger.Debugf("call Service2.APIWithFixedParams out2:%d", out)

		// 3. 固定参数->没有参数 (返回参数数量错误)
		if err := s.Select(rpc.WithServiceName(ServiceNameTest2)).Call(ctx, "APIWithFixedParams", nil, &out); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithFixedParams failed, err:%v", err)
		}
		s.GetLogger().Debugf("==========================================21")
		log.SysLogger.Debugf("call Service2.APIWithFixedParams out3:%d", out)

		// 4. 固定参数->全部参数,不接收返回值 (正常调用,out依然输出3)
		if err := s.Select(rpc.WithServiceName(ServiceNameTest2)).Call(ctx, "APIWithFixedParams", []interface{}{5, 6}, nil); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithFixedParams failed, err:%v", err)
		}
		s.GetLogger().Debugf("==========================================22")
		log.SysLogger.Debugf("call Service2.APIWithFixedParams out4:%d", out)

		//5. 不固定参数->有固定参数和不定参和返回值 给出全部参数 (正常调用)
		if err := s.Select(rpc.WithServiceName(ServiceNameTest2)).Call(ctx, "APIWithFixedAndIndefiniteParams", []interface{}{7, 8, "9", "10"}, &out); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithFixedAndIndefiniteParams failed, err:%v", err)
		}
		s.GetLogger().Debugf("==========================================23")
		log.SysLogger.Debugf("call Service2.APIWithFixedAndIndefiniteParams out5:%d", out)

		// 6. 不固定参数->有固定参数和不定参和返回值 给出全部固定部分,不给变参部分 (正常调用)
		if err := s.Select(rpc.WithServiceName(ServiceNameTest2)).Call(ctx, "APIWithFixedAndIndefiniteParams", []interface{}{11, 12}, &out); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithFixedAndIndefiniteParams failed, err:%v", err)
		}
		s.GetLogger().Debugf("==========================================24")
		log.SysLogger.Debugf("call Service2.APIWithFixedAndIndefiniteParams out6:%d", out)

		// 7. 不固定参数->有固定参数和不定参和返回值 给出部分固定部分,不给变参部分 (返回参数数量错误)
		if err := s.Select(rpc.WithServiceName(ServiceNameTest2)).Call(ctx, "APIWithFixedAndIndefiniteParams", []interface{}{13}, &out); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithFixedAndIndefiniteParams failed, err:%v", err)
		}
		s.GetLogger().Debugf("==========================================25")
		log.SysLogger.Debugf("call Service2.APIWithFixedAndIndefiniteParams out7:%d", out)

		// 8.  不固定参数->有固定参数和不定参和返回值 不给任何参数 (返回参数数量错误)
		if err := s.Select(rpc.WithServiceName(ServiceNameTest2)).Call(ctx, "APIWithFixedAndIndefiniteParams", nil, &out); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithFixedAndIndefiniteParams failed, err:%v", err)
		}
		s.GetLogger().Debugf("==========================================26")
		log.SysLogger.Debugf("call Service2.APIWithFixedAndIndefiniteParams out8:%d", out)

		// 9. 不固定参数->有固定参数和不定参和返回值 给出全部参数,不接收返回值 (正常调用,out依然输出23)
		if err := s.Select(rpc.WithServiceName(ServiceNameTest2)).Call(ctx, "APIWithFixedAndIndefiniteParams", []interface{}{14, 15, "16", "17"}, nil); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithFixedAndIndefiniteParams failed, err:%v", err)
		}
		s.GetLogger().Debugf("==========================================27")
		log.SysLogger.Debugf("call Service2.APIWithFixedAndIndefiniteParams out8:%d", out)

		//9. 没有参数-> 有返回值 (正常调用)
		if err := s.Select(rpc.WithServiceName(ServiceNameTest2)).Call(ctx, "APIWithoutParams", nil, &out); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithoutParams failed, err:%v", err)
		}
		s.GetLogger().Debugf("==========================================28")
		log.SysLogger.Debugf("call Service2.APIWithoutParams out:%d", out)

		// 10. 没有参数-> 给一个参数 (返回参数数量错误)
		if err := s.Select(rpc.WithServiceName(ServiceNameTest2)).Call(ctx, "APIWithoutParams", 1, &out); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithoutParams failed, err:%v", err)
		}
		s.GetLogger().Debugf("==========================================29")
		log.SysLogger.Debugf("call Service2.APIWithoutParams out2:%d", out)

		// 11. 没有参数-> 没有返回值 (正常调用)
		if err := s.Select(rpc.WithServiceName(ServiceNameTest2)).Call(ctx, "APIWithoutParams", nil, nil); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithoutParams failed, err:%v", err)
		}
		s.GetLogger().Debugf("==========================================30")
		log.SysLogger.Debugf("call Service2.APIWithoutParams out3:%d", out)

		//12. 多返回值 -> 全部接收 (正常调用)
		var out1, out2 int
		if err := s.Select(rpc.WithServiceName(ServiceNameTest2)).Call(ctx, "APIWithMultiResults", nil, []interface{}{&out1, &out2}); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithMultiResults failed, err:%v", err)
		}
		s.GetLogger().Debugf("==========================================31")
		// 输出 1, 2
		log.SysLogger.Debugf("call Service2.APIWithMultiResults 1 out1:%d, out2:%d", out1, out2)

		// 13. 多返回值 -> 部分接收 (正常调用)
		if err := s.Select(rpc.WithServiceName(ServiceNameTest2)).Call(ctx, "APIWithMultiResults", nil, []interface{}{&out2}); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithMultiResults failed, err:%v", err)
		}
		s.GetLogger().Debugf("==========================================32")
		// 输出 1, 1
		log.SysLogger.Debugf("call Service2.APIWithMultiResults 2 out1:%d, out2:%d", out1, out2)

		// 14. 多返回值 -> 全都不接收 (正常调用)
		if err := s.Select(rpc.WithServiceName(ServiceNameTest2)).Call(ctx, "APIWithMultiResults", nil, nil); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithMultiResults failed, err:%v", err)
		}
		s.GetLogger().Debugf("==========================================33")
		// 输出 1, 1
		log.SysLogger.Debugf("call Service2.APIWithMultiResults 3 out1:%d, out2:%d", out1, out2)

		// 15. 多返回值 -> 使用错误类型接收 (报错)
		if err := s.Select(rpc.WithServiceName(ServiceNameTest2)).Call(ctx, "APIWithMultiResults", nil, &out1); err != nil {
			log.SysLogger.Errorf("call Service2.APIWithMultiResults failed, err:%v", err)
		}
		s.GetLogger().Debugf("==========================================34")
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

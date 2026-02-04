// Package comm
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/18 0018 20:03
// 最后更新:  yr  2025/7/18 0018 20:03
package comm

import (
	"context"
	"fmt"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/core"
	"github.com/njtc406/emberengine/engine/pkg/core/rpc"
)

type Service2TestModule struct {
	core.Module
}

func (s *Service2TestModule) APISum(ctx context.Context, a, b int) int {
	time.Sleep(time.Second * 200)
	s.WithContext(ctx).Debugf(">>>>>>>>>>> call %s func APISum, a:%d, b:%d", s.GetModuleName(), a, b)
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

func (s *Service2) APITest2(ctx context.Context) {
	//time.Sleep(time.Second * 6) // 模拟耗时操作
	s.WithContext(ctx).Debugf(">>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>call %s func APITest2", s.GetName())
}

//func (s *Service2) APISum(a, b int) int {
//	return a + b
//}

func (s *Service2) APIPrintParams(ctx context.Context, a int, b string) error {
	s.WithContext(ctx).Debugf("call %s func APIPrintParams, a:%d, b:%s", s.GetName(), a, b)
	return nil
	//return fmt.Errorf("test")
}

func (s *Service2) APIPrintIndefiniteParams(ctx context.Context, args ...any) error {
	for _, arg := range args {
		s.WithContext(ctx).Debugf("call %s func APIPrintIndefiniteParams, arg:%+v", s.GetName(), arg)
	}
	return nil
	//return fmt.Errorf("test")
}

func (s *Service2) APIMultiRet(ctx context.Context) (int, string, error) {
	s.WithContext(ctx).Debugf("call %s func APIMultiRet", s.GetName())
	return 1, "2", nil
}

func (s *Service2) APICallback(ctx context.Context) {
	s.WithContext(ctx).Debugf("call %s func APICallback", s.GetName())
	if err := s.Select(rpc.WithName(ServiceNameTest1)).Send(ctx, "APITest1", nil); err != nil {
		s.WithContext(ctx).Errorf("call Service1.APITest1 failed, err:%v", err)
	}
}

func (s *Service2) APIWithFixedParams(ctx context.Context, a, b int) int {
	s.WithContext(ctx).Debugf("call %s func APIWithFixedParams, a:%d, b:%d", s.GetName(), a, b)
	return a + b
}

func (s *Service2) APIWithFixedAndIndefiniteParams(ctx context.Context, a, b int, args ...string) (int, error) {
	s.WithContext(ctx).Debugf("call %s func APIWithFixedAndIndefiniteParams, a:%d, b:%d args:%+v", s.GetName(), a, b, args)
	return a + b, nil
}

func (s *Service2) APIWithMultiResults() (int, int, error) {
	return 1, 2, nil
	return 1, 2, fmt.Errorf("test")
}

func (s *Service2) APIWithoutParams(ctx context.Context) int {
	s.WithContext(ctx).Debugf("call %s func APIWithoutParams", s.GetName())
	return 1
}

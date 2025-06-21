package comm

import (
	"github.com/njtc406/emberengine/engine/pkg/core"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
	"sync/atomic"
	"time"
)

type TestData struct {
	A int
	B string
	C bool
}

type Log struct {
	Opt   int // 操作类型，比如 1 修改A数据 2 删除A数据等等,根据自己的需求定义
	Param interface{}
}

// Service111 测试主从模式服务
type Service111 struct {
	core.Service

	version atomic.Uint64
	a       *TestData
}

func (s *Service111) OnInit() error {
	return nil
}

func (s *Service111) OnStart() error {

	return nil
}

func (s *Service111) OnStarted() error {
	if s.GetPid().GetIsMaster() {

		// 加载数据
		s.a = &TestData{
			A: 1,
			B: "test",
			C: true,
		}
		s.version.Add(1) // 设置版本号

		s.TickerFunc(time.Second, "Service111.Tick", func(timer *timingwheel.Timer, args ...interface{}) {
			// 每秒自动修改数据
		})
		return nil
	} else {

	}
	// 是从服务,那么从主服务获取当前最新数据
	return nil
}

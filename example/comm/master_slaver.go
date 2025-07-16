package comm

import (
	"fmt"
	"github.com/njtc406/emberengine/engine/pkg/core"
	"github.com/njtc406/emberengine/engine/pkg/core/rpc"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
	"github.com/njtc406/emberengine/engine/pkg/utils/util"
	"github.com/njtc406/emberengine/engine/pkg/xcontext"
	"github.com/njtc406/emberengine/example/msg"
	"google.golang.org/protobuf/proto"
	"sync/atomic"
	"time"
)

type TestData struct {
	A       int32
	Version int64
}

type Log struct {
	Opt     int32 // 操作类型，比如 1 修改A数据 2 删除A数据等等,根据自己的需求定义
	Param   int32
	Version int64
}

func (l *Log) ToProto() *msg.TestLog {
	return &msg.TestLog{
		Opt:     l.Opt,
		Param:   l.Param,
		Version: l.Version,
	}
}

const (
	Add int32 = iota // 加
	Sub              // 减
	Mul              // 乘
	Div              // 除
)

// MasterSlaverTest 测试主从模式服务
type MasterSlaverTest struct {
	core.Service

	inited     atomic.Bool
	a          *TestData
	logs       []*Log
	queueCache []*msg.TestLog // 用来缓存数据序列,防止在初始化之前就收到了同步数据
	timer      *timingwheel.Timer
	saveTimer  *timingwheel.Timer
}

func (s *MasterSlaverTest) OnInit() error {
	s.GetEventProcessor().RegEventReceiverFunc(event.ServiceBecomeMaster, s.GetEventHandler(), s.becomeMaster) // 升级为主服务
	s.GetEventProcessor().RegEventReceiverFunc(event.ServiceBecomeSlaver, s.GetEventHandler(), s.becomeSlaver) // 降级为从服务
	s.GetEventProcessor().RegEventReceiverFunc(event.ServiceLoseMaster, s.GetEventHandler(), s.loseMaster)     // 主服务降级
	return nil
}

func (s *MasterSlaverTest) OnStart() error {

	return nil
}

func (s *MasterSlaverTest) OnStarted() error {
	return nil
}

func (s *MasterSlaverTest) OnRelease() {
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if s.saveTimer != nil {
		s.saveTimer.Stop()
		s.saveTimer = nil
	}
	// 保存数据
}

func (s *MasterSlaverTest) becomeMaster(e inf.IEvent) {
	evt := e.(*event.Event)
	oldStateIsMaster := evt.Data.(bool)
	if oldStateIsMaster {
		// 没有变化,自己依然是主服务
		return
	}
	// 升级为主服务
	// TODO 开始正常服务数据和逻辑初始化

	if s.a == nil {
		s.a = &TestData{
			A:       1,
			Version: 1,
		}
	}

	s.inited.Store(true)

	// 注册一个定时任务,模拟主服务数据操作
	s.timer = s.TickerFunc(time.Second, "master tick", s.tick)
	s.saveTimer = s.TickerFunc(time.Second*10, "save all data", s.saveAllData)

	ctx := xcontext.New(nil)
	ctx.SetHeader(def.DefaultPriorityKey, def.PrioritySys)

	// 向所有从服务同步一次完整数据
	if err := s.selectSelfSlavers().Send(ctx, "RpcSyncAllData", s.packageData()); err != nil {
		s.GetLogger().WithContext(ctx).Errorf("sync all data to slaver failed, err:%v", err)
	}

	s.GetLogger().Debugf("master start...")
}

func (s *MasterSlaverTest) selectSelfSlavers() inf.IBus {
	return s.SelectSlavers(
		rpc.WithServiceName(s.GetName()),
		rpc.WithServiceId(s.GetPid().GetServiceId()),
		rpc.WithServerId(s.GetServerId()))
}

func (s *MasterSlaverTest) selectSelfMaster() inf.IBus {
	return s.Select(
		rpc.WithServiceName(s.GetName()),
		rpc.WithServiceId(s.GetPid().GetServiceId()),
		rpc.WithServerId(s.GetServerId()))
}

func (s *MasterSlaverTest) becomeSlaver(e inf.IEvent) {
	// 降级为从服务
	// TODO 屏蔽所有数据操作,只允许使用主服务数据记录回放操作数据
	// ...
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if s.saveTimer != nil {
		s.saveTimer.Stop()
		s.saveTimer = nil
	}
	ctx := xcontext.New(nil)
	ctx.SetHeader(def.DefaultPriorityKey, def.PrioritySys)
	// 从主服务同步一次完整数据
	resp := &msg.TestData{}
	if err := s.selectSelfMaster().Call(ctx, "RpcGetAllData", nil, resp); err != nil {
		s.GetLogger().WithContext(ctx).Errorf("call Service3.RpcGetAllData failed,err:%v ", err)
	}

	s.a = &TestData{
		A:       resp.GetA(),
		Version: resp.GetVersion(),
	}

	s.inited.Store(true)

	// 触发数据回放
	for _, log := range s.queueCache {
		// 对比version
		if log.Version <= s.a.Version {
			// 旧数据,直接丢弃
			continue
		}

		// 更新version
		s.a.Version = log.Version

		s.option(log.Opt, log.Param)
	}
	s.queueCache = s.queueCache[:0]

	s.GetLogger().Debugf("become slaver")
}

func (s *MasterSlaverTest) loseMaster(e inf.IEvent) {
	evt := e.(*event.Event)
	oldStateIsMaster := evt.Data.(bool)
	if oldStateIsMaster {
		// 没有变化,自己依然为主服务
		return
	}
	// 主服务降级
	// TODO 屏蔽所有数据操作,只允许使用主服务数据记录回放操作数据
	// ...
	s.inited.Store(false)

	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if s.saveTimer != nil {
		s.saveTimer.Stop()
		s.saveTimer = nil
	}

	// 存储全量数据,根据version判断是否需要写入
	s.GetLogger().Debugf("lose master...")
}

func (s *MasterSlaverTest) packageData() proto.Message {
	return &msg.TestData{
		A:       s.a.A,
		Version: s.a.Version,
	}
}

// RpcSyncAllData 主服务向从服务同步最新全量数据
func (s *MasterSlaverTest) RpcSyncAllData(req *msg.TestData) error {
	if req == nil {
		return def.ErrParamNotMatch
	}
	if s.GetPid().GetIsMaster() {
		// 不能向主服务同步全量数据
		return fmt.Errorf("不能向主服务同步全量数据")
	}
	s.a = &TestData{
		A:       req.GetA(),
		Version: req.GetVersion(),
	}
	return nil
}

// RpcGetAllData 从服务向主服务请求最新全量数据
func (s *MasterSlaverTest) RpcGetAllData() (*msg.TestData, error) {
	if !s.inited.Load() {
		// 主服务还未加载完成
		return nil, fmt.Errorf("主服务还未加载完成")
	}
	if !s.GetPid().GetIsMaster() {
		return nil, fmt.Errorf("当前服务非主服务")
	}
	s.GetLogger().Debugf("slaver get master all data, version: %v", s.a.Version)
	return &msg.TestData{
		A:       s.a.A,
		Version: s.a.Version,
	}, nil
}

func (s *MasterSlaverTest) RpcSyncLog(req *msg.TestLog) error {
	if !s.inited.Load() {
		// 从服务还为获取到完整的全量数据,放入缓存
		s.queueCache = append(s.queueCache, req)
		return nil
	}

	// 对比version
	if req.Version <= s.a.Version {
		// 旧数据,直接丢弃
		return nil
	}

	// 更新version
	s.a.Version = req.Version

	s.GetLogger().Debugf("slaver receive sync log:%v", req)
	s.option(req.Opt, req.Param)
	return nil
}

func (s *MasterSlaverTest) tick(timer *timingwheel.Timer, args ...interface{}) {
	opt := util.RandN[int32](4)     // 产生一个0-3的操作
	param := util.RandN[int32](100) // 随机产生一个参数

	s.a.Version++ // 版本号自增

	log := &Log{
		Opt:     opt,
		Param:   param,
		Version: s.a.Version,
	}
	// 写入日志
	s.logs = append(s.logs, log)

	// 变更数据
	s.option(opt, param)

	// 同步所有从服务
	ctx := xcontext.New(nil)
	if err := s.selectSelfSlavers().Send(ctx, "RpcSyncLog", log.ToProto()); err != nil {
		s.GetLogger().WithContext(ctx).Errorf("sync all data to slaver failed, err:%v", err)
	}

	// TODO 如果有任何需要操作其他的东西,都只有主服务可以进行后续,从服务只做数据更新
}

func (s *MasterSlaverTest) saveAllData(timer *timingwheel.Timer, args ...interface{}) {
	// 定时做完整数据镜像,并清空log

	s.logs = s.logs[:0]
}

func (s *MasterSlaverTest) option(opt, param int32) {
	// 执行日志
	switch opt {
	case Add:
		s.a.A += param
	case Sub:
		s.a.A -= param
	case Mul:
		s.a.A *= param
	case Div:
		if param == 0 {
			param = 1
		}
		s.a.A /= param
	}

	// 打印当前最新数据
	s.GetLogger().Debugf(">>>>>>>>>>>>>>>>>>>>current data:%v", s.a)
}

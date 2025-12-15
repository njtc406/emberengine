package comm

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/cluster/leadership"
	"github.com/njtc406/emberengine/engine/pkg/core"
	"github.com/njtc406/emberengine/engine/pkg/core/rpc"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
	"github.com/njtc406/emberengine/engine/pkg/utils/util"
	"github.com/njtc406/emberengine/engine/pkg/utils/xcontext"
	"github.com/njtc406/emberengine/example/msg"
	"google.golang.org/protobuf/proto"
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
//
// 这个示例的目的：演示“主从切换事件 + 主写从跟 + 失主即停(guard)”这套用法。
//
// 它解决/演示的问题：
// - master 产生写入并复制到所有 slaver（最终让 slaver 跟上 master 的状态）
// - 使用版本号(version)保证从端回放幂等（旧日志丢弃）
// - 在失去 master 后，立即停止所有 master-only 的定时任务/后台循环
// - 即使存在极短的竞态窗口（timer 回调已经被调度），也通过 guard 二次检查避免副作用
type MasterSlaverTest struct {
	core.Service

	inited        atomic.Bool
	guard         *leadership.Guard
	a             *TestData
	logs          []*Log
	queueCache    []*msg.TestLog // 用来缓存数据序列,防止在初始化之前就收到了同步数据
	masterTimerId uint64
	guardDemoId   uint64
	slaverTimerId uint64
}

func (s *MasterSlaverTest) OnInit() error {
	// LeadershipGuard 是框架层提供的“失主即停”工具：
	// - 通过 ServiceBecomeMaster/LoseMaster/... 事件驱动
	// - 暴露一个 leader-only 的 ctx（失去主权会立即 cancel）
	// - 暴露 fencing token：epoch（单调递增）用于上层做围栏
	//
	// 把任何 master-only 的 goroutine 都绑定到 guard.Ctx() 即可做到失主即停。
	s.guard = leadership.NewGuard(context.Background())

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
	s.CancelTimer(s.masterTimerId)
	s.masterTimerId = 0
	s.CancelTimer(s.guardDemoId)
	s.guardDemoId = 0
	s.CancelTimer(s.slaverTimerId)
	s.slaverTimerId = 0
	// 保存数据
}

func (s *MasterSlaverTest) becomeMaster(ctx context.Context, e inf.IEvent) {
	evt := e.(*event.Event)
	if s.guard != nil {
		s.guard.OnEvent(ctx, e)
	}
	stateData := evt.Data.(*event.MasterStateData)
	if stateData.OldStateIsMaster {
		// 没有变化,自己依然是主服务（可能是重复通知）
		s.Debugf("still master, epoch=%v", stateData.NewEpoch)
		return
	}

	// 框架层会把 fencing token (epoch) 放进事件 data，业务可以用于围栏。
	// 注意：框架只能提供 token 和失主 cancel；具体副作用边界仍需业务用 epoch 做校验。
	s.Debugf("become master, prevEpoch=%v newEpoch=%v", stateData.PrevEpoch, stateData.NewEpoch)

	// 升级为主服务
	// TODO 开始正常服务数据和逻辑初始化

	if s.a == nil {
		s.a = &TestData{
			A:       1,
			Version: 1,
		}
	}

	s.inited.Store(true)

	// 示例：如何把“任何 master-only 的后台周期任务”绑定到 guard.Ctx()。
	//
	// 模拟一个后台任务
	if s.guard != nil {
		go func() {
			<-s.guard.Ctx().Done()
			s.Debugf("master-only job done, epoch=%v", s.guard.Epoch())
		}()
	}

	// 注册定时任务：模拟“只有 master 才会持续产生写入”。
	// 真实业务里这可能是：撮合/调度/对外写接口/扫描任务等。
	t1, err := s.TickerFunc(time.Second, "master tick", s.tick)
	if err != nil {
		s.Panicf("create master tick timer failed, err:%v", err)
	}
	s.masterTimerId = t1

	// 注册定时任务：模拟周期性做快照/落盘并截断日志。
	// 真实业务里可能是：定期把状态落库/写文件，并清理已确认的操作日志。
	t2, err := s.TickerFunc(time.Second*10, "save all data", s.saveAllData)
	if err != nil {
		s.Panicf("create save all data timer failed, err:%v", err)
	}
	s.slaverTimerId = t2

	xctx := xcontext.New(ctx)
	xctx.AddHeader(def.DefaultPriorityKey, def.PrioritySys)

	// 向所有从服务同步一次完整数据
	if err := s.selectSelfSlavers().Send(xctx, "RpcSyncAllData", s.packageData()); err != nil {
		s.WithContext(xctx).Errorf("sync all data to slaver failed, err:%v", err)
	}

	s.Debugf("master start...")
}

func (s *MasterSlaverTest) selectSelfSlavers() inf.IBus {
	return s.SelectSlavers(
		rpc.WithName(s.GetName()),
		rpc.WithSid(s.GetPid().GetServiceId()),
		rpc.WithServerId(s.GetServerId()))
}

func (s *MasterSlaverTest) selectSelfMaster() inf.IBus {
	return s.Select(
		rpc.WithName(s.GetName()),
		rpc.WithSid(s.GetPid().GetServiceId()),
		rpc.WithServerId(s.GetServerId()))
}

func (s *MasterSlaverTest) becomeSlaver(ctx context.Context, e inf.IEvent) {
	evt := e.(*event.Event)
	if s.guard != nil {
		s.guard.OnEvent(ctx, e)
	}
	stateData := evt.Data.(*event.MasterStateData)
	s.Debugf("Become slaver, prevEpoch=%v newEpoch=%v", stateData.PrevEpoch, stateData.NewEpoch)

	// 降级为从服务
	// TODO 屏蔽所有数据操作,只允许使用主服务数据记录回放操作数据
	// ...
	s.CancelTimer(s.masterTimerId)
	s.masterTimerId = 0
	s.CancelTimer(s.guardDemoId)
	s.guardDemoId = 0
	s.CancelTimer(s.slaverTimerId)
	s.slaverTimerId = 0

	xctx := xcontext.New(ctx)
	xctx.AddHeader(def.DefaultPriorityKey, def.PrioritySys)
	// 从主服务同步一次完整数据
	resp := &msg.TestData{}
	if err := s.selectSelfMaster().Call(xctx, "RpcGetAllData", nil, resp); err != nil {
		s.WithContext(xctx).Errorf("call Service3.RpcGetAllData failed,err:%v ", err)
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

}

func (s *MasterSlaverTest) loseMaster(ctx context.Context, e inf.IEvent) {
	evt := e.(*event.Event)
	if s.guard != nil {
		s.guard.OnEvent(ctx, e)
	}
	stateData := evt.Data.(*event.MasterStateData)
	if !stateData.OldStateIsMaster {
		// 本来就不是 master，就不需要做降级动作
		return
	}
	s.Debugf("lose master, prevEpoch=%v newEpoch=%v", stateData.PrevEpoch, stateData.NewEpoch)
	// 主服务降级
	// TODO 屏蔽所有数据操作,只允许使用主服务数据记录回放操作数据
	// ...
	s.inited.Store(false)

	s.CancelTimer(s.masterTimerId)
	s.masterTimerId = 0
	s.CancelTimer(s.guardDemoId)
	s.guardDemoId = 0
	s.CancelTimer(s.slaverTimerId)
	s.slaverTimerId = 0

	// 存储全量数据,根据version判断是否需要写入
	s.Debugf("lose master...")
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
	s.Debugf("slaver get master all data, version: %v", s.a.Version)
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

	s.Debugf("slaver receive sync log:%v", req)
	s.option(req.Opt, req.Param)
	return nil
}

func (s *MasterSlaverTest) tick(timer *timingwheel.Timer, args ...interface{}) error {
	// 防御式检查：即使 timer 因为调度/竞态晚到，也不要在非 master 上产生副作用。
	if s.guard != nil && !s.guard.IsLeader() {
		return nil
	}

	// tick 在模拟什么？
	// - master 周期性产生一条“写操作”（日志），并同步给 slaver 回放。
	// - 这不是 Raft；这里只是演示主从使用方式（主写、从跟、版本幂等）。
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
		s.WithContext(ctx).Errorf("sync all data to slaver failed, err:%v", err)
	}

	// TODO 如果有任何需要操作其他的东西,都只有主服务可以进行后续,从服务只做数据更新
	return nil
}

func (s *MasterSlaverTest) saveAllData(timer *timingwheel.Timer, args ...interface{}) error {
	if s.guard != nil && !s.guard.IsLeader() {
		return nil
	}
	// saveAllData 在模拟什么？
	// - 周期性做“全量数据镜像/快照”
	// - 随后清理已经包含在快照里的增量日志
	//
	// 为什么需要它？
	// - 长时间运行下日志会无限增长，真实系统必须做快照/落盘与截断
	// - 示例不落盘，只用清空 logs 表达“这一批已固化，不再需要重复回放”

	s.logs = s.logs[:0]
	return nil
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
	s.Debugf(">>>>>>>>>>>>>>>>>>>>current data:%v", s.a)
}

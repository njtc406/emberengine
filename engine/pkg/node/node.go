package node

import (
	"fmt"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/actor/mailbox/job"
	"github.com/njtc406/emberengine/engine/pkg/cluster"
	etcddiscovery "github.com/njtc406/emberengine/engine/pkg/cluster/discovery/etcd"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/core/rpc"
	"github.com/njtc406/emberengine/engine/pkg/event"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/monitor"
	"github.com/njtc406/emberengine/engine/pkg/plugins"
	"github.com/njtc406/emberengine/engine/pkg/profiler"
	"github.com/njtc406/emberengine/engine/pkg/router"
	"github.com/njtc406/emberengine/engine/pkg/rpc/client"
	"github.com/njtc406/emberengine/engine/pkg/rpc/client/pool"
	"github.com/njtc406/emberengine/engine/pkg/rpc/message/msgbus"
	"github.com/njtc406/emberengine/engine/pkg/rpc/message/msgenvelope"
	remotehandler "github.com/njtc406/emberengine/engine/pkg/rpc/remote/handler"
	"github.com/njtc406/emberengine/engine/pkg/services"
	"github.com/njtc406/emberengine/engine/pkg/utils/asynclib"
	"github.com/njtc406/emberengine/engine/pkg/utils/codec"
	"github.com/njtc406/emberengine/engine/pkg/utils/dedup"
	"github.com/njtc406/emberengine/engine/pkg/utils/pid"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
	"github.com/njtc406/emberengine/engine/pkg/utils/title"
	"github.com/njtc406/emberengine/engine/pkg/utils/translate"
	"github.com/njtc406/emberengine/engine/pkg/utils/version"

	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

// Node 是 EmberEngine 的自包含运行时实例。
// Phase 1: Node 持有 config、log、asynclib、timingwheel、dedup 的独立实例。
// Phase 2: Node 持有 monitor、event、cluster、services 的独立实例。
// Phase 3: Node 持有 PoolManager、SenderManager、MethodIndex 的独立实例。
// 所有运行时组件均由 Node 持有，不使用全局兼容入口。
type Node struct {
	// 基本信息
	version   string
	confPath  string
	hooks     []HookFun
	extra     map[any]any
	startTime time.Time

	// ====== Phase 1: 从全局收归的组件 ======

	// 配置（由 Node 独立持有）
	Config *config.Config

	// 日志
	*log.Logger

	// 协程池（原 asynclib.antsPool）
	AntsPool *asynclib.Pool

	// 时间轮（原 timingwheel.globTW）
	TimingWheel *timingwheel.TimingWheel

	// 去重器（原 dedup.duplicator）
	DeDuplicator inf.IDeDuplicator

	// ====== Phase 2: 核心组件 ======

	// RPC 监控（原 monitor.rpcMonitor）
	RpcMonitor *monitor.RpcMonitor

	// 事件总线（原 event.bus）
	EventBus *event.Bus

	// 集群（原 cluster.cluster）
	Cluster *cluster.Cluster

	// 服务管理器（原 services 包级 runServices）
	ServiceMgr *services.ServiceManager

	// ====== Phase 3: RPC 层组件 ======

	// RPC 连接池管理器（原 pool.globalPoolManager）
	PoolManager *pool.PoolManager

	// RPC Sender 管理器（原 client 包级 senderMap/senderHandlerMap）
	SenderMgr *client.SenderManager

	// 方法前缀索引（原 core/rpc 包级 apiPrefixIndex 等）
	MethodIndex *rpc.MethodIndex

	// MessageBus 工厂（用于隔离每个 Node 的 bus pool/logger/monitor/timeout）
	BusFactory *msgbus.MessageBusFactory

	// ====== Phase 4: 辅助组件 ======

	// Profiler 注册中心（原 profiler 包级 mapProfiler）
	ProfilerRegistry *profiler.Registry

	// 插件管理器（原 plugins 包级 pluginMap）
	PluginManager *plugins.PluginManager

	// 路由器（原 router 直接依赖 endpoints.GetEndpointManager）
	Router *router.Router

	// 停止标志（防止 Stop() 重复调用）
	stopped atomic.Bool

	// 启动成功后用于 Stop() 的清理步骤（按注册顺序，Stop 时逆序执行）
	stopCleanups []nodeCleanup
}

type nodeCleanup struct {
	name          string
	fn            func()
	includeInStop bool
}

func appendCleanup(cleanups *[]nodeCleanup, name string, includeInStop bool, fn func()) {
	if fn == nil {
		return
	}
	*cleanups = append(*cleanups, nodeCleanup{name: name, fn: fn, includeInStop: includeInStop})
}

func runCleanupReverse(cleanups []nodeCleanup) {
	for i := len(cleanups) - 1; i >= 0; i-- {
		if cleanups[i].fn != nil {
			cleanups[i].fn()
		}
	}
}

func filterStopCleanups(cleanups []nodeCleanup) []nodeCleanup {
	result := make([]nodeCleanup, 0, len(cleanups))
	for _, step := range cleanups {
		if step.includeInStop {
			result = append(result, step)
		}
	}
	return result
}

type profilerRegistryAdapter struct {
	registry *profiler.Registry
}

func (a *profilerRegistryAdapter) RegProfiler(name string, logger log.ILoggerX) inf.IProfiler {
	if a == nil || a.registry == nil {
		return nil
	}
	p := a.registry.RegProfiler(name, logger)
	if p == nil {
		return nil
	}
	return profiler.NewAdapter(p)
}

func (a *profilerRegistryAdapter) UnRegProfiler(name string) {
	if a == nil || a.registry == nil {
		return
	}
	a.registry.UnRegProfiler(name)
}

func New() *Node {
	return &Node{}
}

// ── INodeContext 接口实现 ──

func (n *Node) GetConfig() inf.INodeConfig           { return n.Config }
func (n *Node) GetLogger() log.ILoggerX              { return n.Logger }
func (n *Node) GetAntsPool() inf.INodePool           { return n.AntsPool }
func (n *Node) GetTimingWheel() inf.INodeTimingWheel { return n.TimingWheel }
func (n *Node) GetDeDuplicator() inf.IDeDuplicator   { return n.DeDuplicator }
func (n *Node) GetNodeId() string                    { return n.Config.NodeConf.NodeId }
func (n *Node) GetNodeType() string                  { return n.Config.NodeConf.NodeType }
func (n *Node) GetNodeUid() string {
	if n.Cluster != nil {
		em := n.Cluster.GetEndpointManager()
		if em != nil {
			if nodeUid := em.GetNodeUid(); nodeUid != "" {
				return nodeUid
			}
		}
	}
	if n.Config != nil && n.Config.NodeConf != nil {
		return n.Config.NodeConf.NodeType + "_" + n.Config.NodeConf.NodeId
	}
	return ""
}

func (n *Node) IsClusterMode() bool {
	if n.Cluster == nil {
		return false
	}
	return n.Cluster.IsClusterMode()
}

func (n *Node) GetEndpointManager() inf.INodeEndpointManager {
	if n.Cluster == nil {
		return nil
	}
	return n.Cluster.GetEndpointManager()
}

func (n *Node) GetEventBus() inf.INodeEventBus {
	if n.EventBus == nil {
		return nil
	}
	return n.EventBus
}

func (n *Node) GetRouter() inf.INodeRouter {
	if n.Router == nil {
		return nil
	}
	return n.Router
}

func (n *Node) GetProfilerRegistry() inf.INodeProfilerRegistry {
	if n.ProfilerRegistry == nil {
		return nil
	}
	return &profilerRegistryAdapter{registry: n.ProfilerRegistry}
}

func (n *Node) GetMethodIndex() inf.INodeMethodIndex {
	if n.MethodIndex == nil {
		return nil
	}
	return n.MethodIndex
}

// 编译期检查：确保 Node 实现了 INodeContext
var _ inf.INodeContext = (*Node)(nil)

func fixVersion(v string) string {
	if v == "" {
		return version.Version
	}
	return v
}

func (n *Node) runHookSafe(hook HookFun) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("hook panic: %v\n%s", r, debug.Stack())
		}
	}()
	return hook(n, n.extra)
}

func (n *Node) Start(opts ...StartOption) (retNode *Node, retErr error) {
	// ── cleanups 栈：记录已完成的初始化步骤，失败时逆序回滚 ──
	var cleanups []nodeCleanup
	defer func() {
		if retErr != nil {
			runCleanupReverse(cleanups)
			n.stopCleanups = nil
		}
	}()

	// 0. 应用选项
	param := StartParam{}
	for _, f := range opts {
		f(&param)
	}
	n.version = fixVersion(param.Version)
	n.confPath = param.ConfPath
	n.hooks = param.Hooks
	n.extra = param.Extra

	// 0.1 语言设置（全局共享，仅第一个 Node 生效）
	if param.Language > 0 {
		translate.SetLanguage(param.Language)
	}

	// 0.2 打印版本信息
	title.EchoTitle(n.version)

	// ==============================
	// 1. 配置（最先初始化 — 其他一切依赖配置）
	// ==============================
	n.Config = config.NewConfig()
	if err := n.Config.Load(n.confPath); err != nil {
		return nil, fmt.Errorf("config load: %w", err)
	}

	// ==============================
	// 2. 日志（第二个初始化 — 后续组件需要日志）
	// ==============================
	var err error
	n.Logger, err = log.NewLogger(n.Config.SystemLogger, n.Config.IsDebug())
	if err != nil {
		return nil, fmt.Errorf("logger init: %w", err)
	}
	appendCleanup(&cleanups, "close logger", false, func() { n.Logger.Close() })

	n.Info("-------->system log init ok<---------")

	// ==============================
	// 3. 基础设施层
	// ==============================
	job.SetDebug(n.Config.IsDebug())
	codec.SetDebug(n.Config.IsDebug())
	msgenvelope.SetDebug(n.Config.IsDebug())
	monitor.SetDebug(n.Config.IsDebug())
	etcddiscovery.SetDebug(n.Config.IsDebug())

	n.AntsPool, err = asynclib.NewPool(n.Config.NodeConf.AntsPoolSize)
	if err != nil {
		return nil, fmt.Errorf("ants pool: %w", err)
	}
	appendCleanup(&cleanups, "release async pool", true, func() { n.AntsPool.Release() })

	twConf := n.Config.NodeConf.TimingWheelConf
	interval := twConf.Interval
	if interval <= 0 {
		interval = time.Second
	}
	n.TimingWheel = timingwheel.NewTimingWheel(
		interval,
		twConf.WheelSize,
		log.NewLoggerX(n.Logger, log.Fields{"pkg": "timingwheel"}),
	)
	n.TimingWheel.Start()
	appendCleanup(&cleanups, "stop timing wheel", true, func() { n.TimingWheel.Stop() })

	n.DeDuplicator, err = dedup.NewDeDuplicator(n.Config.NodeConf.DeDuplicatorConf)
	if err != nil {
		return nil, fmt.Errorf("dedup: %w", err)
	}
	appendCleanup(&cleanups, "close deduplicator", true, func() { n.DeDuplicator.Close() })

	// ==============================
	// 4. RPC 监控
	// ==============================
	n.RpcMonitor = monitor.NewRpcMonitor().Init(
		n.Config.NodeConf.RpcMonitorConf,
		n.Logger,
		n.TimingWheel,
		n.AntsPool,
	)

	// 记录 PID
	pid.RecordPID(n.Config.NodeConf.PVPath, n.Config.NodeConf.NodeId, n.Config.NodeConf.NodeType)
	appendCleanup(&cleanups, "delete pid file", false, func() {
		pid.DeletePID(n.Config.NodeConf.PVPath, n.Config.NodeConf.NodeId, n.Config.NodeConf.NodeType)
	})

	// 启动 RPC 监控
	if err := n.RpcMonitor.Start(); err != nil {
		return nil, fmt.Errorf("rpc monitor start: %w", err)
	}
	appendCleanup(&cleanups, "stop rpc monitor", true, func() { n.RpcMonitor.Stop() })

	// ==============================
	// 4.5 RPC 层（Phase 3 组件）
	// ==============================

	// 连接池管理器
	n.PoolManager = pool.NewPoolManager(n.Logger)
	appendCleanup(&cleanups, "close pool manager", true, func() { n.PoolManager.Close() })

	// Sender 管理器
	var natsConf *config.NatsConf
	grpcConnNum := 0
	if n.Config != nil && n.Config.NodeConf != nil && n.Config.NodeConf.EventBusConf != nil {
		natsConf = n.Config.NodeConf.EventBusConf.NatsConf
	}
	if n.Config != nil && n.Config.NodeConf != nil {
		grpcConnNum = n.Config.NodeConf.GrpcSenderConnNum
	}
	n.SenderMgr = client.NewSenderManager(n.PoolManager, n.Logger, n.RpcMonitor, natsConf, grpcConnNum)
	remoteMsgHandler := remotehandler.NewHandler(n.RpcMonitor, n.Logger, n.DeDuplicator)
	appendCleanup(&cleanups, "close sender manager", true, func() { n.SenderMgr.Close() })

	// 方法前缀索引
	n.MethodIndex = rpc.NewMethodIndex()

	// MessageBus 工厂（每个 Node 独立）
	n.BusFactory = msgbus.NewMessageBusFactory(
		n.Config.NodeConf.BusPoolSize,
		n.Logger,
		n.RpcMonitor,
		n.Config.GetDefaultRpcTimeout(),
	)

	// ==============================
	// 4.6 辅助组件（Phase 4）
	// ==============================

	n.ProfilerRegistry = profiler.NewRegistry()

	n.PluginManager = plugins.NewPluginManager()

	// ==============================
	// 5. 集群 & 事件总线
	// ==============================
	n.Cluster = cluster.NewCluster()
	if err := n.Cluster.Init(n.Config.ClusterConf, n.Logger, n.SenderMgr, remoteMsgHandler, natsConf, n.BusFactory); err != nil {
		return nil, fmt.Errorf("cluster init: %w", err)
	}
	n.Router = router.NewRouter(n.Cluster.GetEndpointManager())
	if err := n.Cluster.Start(); err != nil {
		return nil, fmt.Errorf("cluster start: %w", err)
	}
	appendCleanup(&cleanups, "close cluster", true, func() { n.Cluster.Close() })

	n.EventBus = event.NewEventBus()
	if err := n.EventBus.Init(n.Config.NodeConf.EventBusConf, n.Logger); err != nil {
		return nil, fmt.Errorf("event bus init: %w", err)
	}
	appendCleanup(&cleanups, "stop event bus", true, func() { n.EventBus.Stop() })

	// ==============================
	// 6. 用户钩子
	// ==============================
	for i, f := range n.hooks {
		if f == nil {
			continue
		}
		if err := n.runHookSafe(f); err != nil {
			return nil, fmt.Errorf("run hook[%d]: %w", i, err)
		}
	}

	// ==============================
	// 7. 服务（最后启动 — 依赖以上所有组件）
	// ==============================
	n.ServiceMgr = services.NewServiceManager(n.Logger)
	n.ServiceMgr.SetRuntimeDeps(n.Cluster, n.Cluster.GetEndpointManager(), n.ProfilerRegistry, n.Router)
	n.ServiceMgr.SetNodeContext(n)
	if err := n.ServiceMgr.Init(n.Config.ServiceConf); err != nil {
		return nil, fmt.Errorf("service manager init: %w", err)
	}
	if err := n.ServiceMgr.Start(); err != nil {
		return nil, fmt.Errorf("service manager start: %w", err)
	}
	appendCleanup(&cleanups, "stop all services", true, func() { n.ServiceMgr.StopAll() })

	n.stopCleanups = filterStopCleanups(cleanups)

	n.startTime = time.Now()
	return n, nil
}

func (n *Node) Stop() {
	// 幂等保护
	if !n.stopped.CompareAndSwap(false, true) {
		return
	}

	defer pid.DeletePID(n.Config.NodeConf.PVPath, n.Config.NodeConf.NodeId, n.Config.NodeConf.NodeType)

	n.Info("==================>>begin stop<<==================")

	for i := len(n.stopCleanups) - 1; i >= 0; i-- {
		step := n.stopCleanups[i]
		n.Infof("stopping: %s", step.name)
		if step.fn != nil {
			step.fn()
		}
	}
	n.stopCleanups = nil

	// 9. 关闭日志（最后 — 确保以上步骤的日志都能输出）
	if n.Config.IsDebug() {
		if dump := msgenvelope.DumpMetaPoolLeaks(20); dump != "" {
			n.Warn(dump)
		}
	}
	n.Info("[9/9] Node stopped, closing logger...")
	n.Logger.Close()

	// 优雅退出
	title.GracefulExit(time.Since(n.startTime), n.version)
}

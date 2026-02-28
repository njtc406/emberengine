package node

import (
	"fmt"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/cluster"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/core/rpc"
	"github.com/njtc406/emberengine/engine/pkg/event"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/monitor"
	"github.com/njtc406/emberengine/engine/pkg/rpc/client"
	"github.com/njtc406/emberengine/engine/pkg/rpc/client/pool"
	"github.com/njtc406/emberengine/engine/pkg/rpc/message/msgbus"
	"github.com/njtc406/emberengine/engine/pkg/rpc/message/msgenvelope"
	"github.com/njtc406/emberengine/engine/pkg/services"
	"github.com/njtc406/emberengine/engine/pkg/utils/asynclib"
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
// 全局兼容：各组件的 SetXxx/GetXxx 仍可用，但 Deprecated。
type Node struct {
	// 基本信息
	version   string
	confPath  string
	hooks     []HookFun
	extra     map[any]any
	startTime time.Time

	// ====== Phase 1: 从全局收归的组件 ======

	// 配置（原 config.Conf）
	Config *config.Config

	// 日志（原 log.SysLogger）— Node 嵌入 Logger，可直接 n.Info(...)
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

	// 停止标志（防止 Stop() 重复调用）
	stopped atomic.Bool
}

func New() *Node {
	return &Node{}
}

// ── INodeContext 接口实现 ──

func (n *Node) GetConfig() *config.Config          { return n.Config }
func (n *Node) GetLogger() *log.Logger             { return n.Logger }
func (n *Node) GetAntsPool() *asynclib.Pool        { return n.AntsPool }
func (n *Node) GetTimingWheel() any                { return n.TimingWheel }
func (n *Node) GetDeDuplicator() inf.IDeDuplicator { return n.DeDuplicator }
func (n *Node) GetNodeId() string                  { return n.Config.NodeConf.NodeId }
func (n *Node) GetNodeType() string                { return n.Config.NodeConf.NodeType }

// 编译期检查：确保 Node 实现了 INodeContext
var _ inf.INodeContext = (*Node)(nil)

func fixVersion(v string) string {
	if v == "" {
		return version.Version
	}
	return v
}

func (n *Node) Start(opts ...StartOption) (retNode *Node, retErr error) {
	// ── cleanups 栈：记录已完成的初始化步骤，失败时逆序回滚 ──
	var cleanups []func()
	defer func() {
		if retErr != nil {
			for i := len(cleanups) - 1; i >= 0; i-- {
				cleanups[i]()
			}
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
	// 临时全局兼容：设置 config.Conf 供尚未改造的包使用
	config.SetConf(n.Config)

	// ==============================
	// 2. 日志（第二个初始化 — 后续组件需要日志）
	// ==============================
	var err error
	n.Logger, err = log.NewLogger(n.Config.SystemLogger, n.Config.IsDebug())
	if err != nil {
		return nil, fmt.Errorf("logger init: %w", err)
	}
	cleanups = append(cleanups, func() { n.Logger.Close() })
	// 临时全局兼容：设置 log.SysLogger
	log.SetSysLogger(n.Logger)

	n.Info("-------->system log init ok<---------")

	// ==============================
	// 3. 基础设施层
	// ==============================
	n.AntsPool, err = asynclib.NewPool(n.Config.NodeConf.AntsPoolSize)
	if err != nil {
		return nil, fmt.Errorf("ants pool: %w", err)
	}
	cleanups = append(cleanups, func() { n.AntsPool.Release() })
	// 临时全局兼容
	asynclib.SetDefaultPool(n.AntsPool)

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
	cleanups = append(cleanups, func() { n.TimingWheel.Stop() })
	// 临时全局兼容
	timingwheel.SetDefaultTimingWheel(n.TimingWheel)

	n.DeDuplicator, err = dedup.NewDeDuplicator(n.Config.NodeConf.DeDuplicatorConf)
	if err != nil {
		return nil, fmt.Errorf("dedup: %w", err)
	}
	cleanups = append(cleanups, func() { n.DeDuplicator.Close() })
	// 临时全局兼容
	dedup.SetDefaultDeDuplicator(n.DeDuplicator)

	// ==============================
	// 4. RPC 监控
	// ==============================
	n.RpcMonitor = monitor.NewRpcMonitor().Init(
		n.Config.NodeConf.RpcMonitorConf,
		n.Logger,
		n.TimingWheel,
		n.AntsPool,
	)
	// 临时全局兼容
	monitor.SetRpcMonitor(n.RpcMonitor)

	// 记录 PID
	pid.RecordPID(n.Config.NodeConf.PVPath, n.Config.NodeConf.NodeId, n.Config.NodeConf.NodeType)
	cleanups = append(cleanups, func() {
		pid.DeletePID(n.Config.NodeConf.PVPath, n.Config.NodeConf.NodeId, n.Config.NodeConf.NodeType)
	})

	// 启动 RPC 监控
	n.RpcMonitor.Start()
	cleanups = append(cleanups, func() { n.RpcMonitor.Stop() })

	// ==============================
	// 4.5 RPC 层（Phase 3 组件）
	// ==============================

	// 连接池管理器
	n.PoolManager = pool.NewPoolManager(n.Logger)
	// 临时全局兼容
	pool.SetGlobalPoolManager(n.PoolManager)
	cleanups = append(cleanups, func() { n.PoolManager.Close() })

	// Sender 管理器
	n.SenderMgr = client.NewSenderManager(n.PoolManager, n.Logger)
	// 临时全局兼容
	client.SetSenderManager(n.SenderMgr)
	cleanups = append(cleanups, func() { n.SenderMgr.Close() })

	// 方法前缀索引
	n.MethodIndex = rpc.NewMethodIndex()
	// 临时全局兼容
	rpc.SetMethodIndex(n.MethodIndex)

	// MessageBus 对象池
	msgbus.InitBusPool(n.Config.NodeConf.BusPoolSize)

	// ==============================
	// 5. 集群 & 事件总线
	// ==============================
	n.Cluster = cluster.NewCluster()
	n.Cluster.Init(n.Config.ClusterConf, n.Logger)
	// 临时全局兼容
	cluster.SetCluster(n.Cluster)
	n.Cluster.Start()
	cleanups = append(cleanups, func() { n.Cluster.Close() })

	n.EventBus = event.NewEventBus()
	n.EventBus.Init(n.Config.NodeConf.EventBusConf, n.Logger)
	// 临时全局兼容
	event.SetEventBus(n.EventBus)
	cleanups = append(cleanups, func() { n.EventBus.Stop() })

	// ==============================
	// 6. 用户钩子
	// ==============================
	for _, f := range n.hooks {
		f(n.extra)
	}

	// ==============================
	// 7. 服务（最后启动 — 依赖以上所有组件）
	// ==============================
	n.ServiceMgr = services.NewServiceManager(n.Logger)
	n.ServiceMgr.Init(n.Config.ServiceConf)
	// 临时全局兼容
	services.SetServiceManager(n.ServiceMgr)
	n.ServiceMgr.Start()
	cleanups = append(cleanups, func() { n.ServiceMgr.StopAll() })

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

	// 1. 停止所有服务（逆序）
	n.Info("[1/9] Stopping all services...")
	n.ServiceMgr.StopAll()
	n.Info("[1/9] All services stopped")

	// 2. 关闭事件总线
	n.Info("[2/9] Stopping event bus...")
	n.EventBus.Stop()
	n.Info("[2/9] Event bus stopped")

	// 3. 关闭集群
	n.Info("[3/9] Closing cluster...")
	n.Cluster.Close()
	n.Info("[3/9] Cluster closed")

	// 4. 关闭 Sender 管理器 & 连接池
	n.Info("[4/9] Closing sender manager...")
	n.SenderMgr.Close()
	n.Info("[4/9] Sender manager closed")

	n.Info("[4/9] Closing pool manager...")
	n.PoolManager.Close()
	n.Info("[4/9] Pool manager closed")

	// 5. 停止 RPC 监控
	n.Info("[5/9] Stopping RPC monitor...")
	n.RpcMonitor.Stop()
	n.Info("[5/9] RPC monitor stopped")

	// 6. 关闭去重器
	n.Info("[6/9] Closing deduplicator...")
	n.DeDuplicator.Close()
	n.Info("[6/9] Deduplicator closed")

	// 7. 停止时间轮
	n.Info("[7/9] Stopping timing wheel...")
	n.TimingWheel.Stop()
	n.Info("[7/9] Timing wheel stopped")

	// 8. 释放协程池
	n.Info("[8/9] Releasing async pool...")
	n.AntsPool.Release()
	n.Info("[8/9] Async pool released")

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

// Start 是包级便捷函数：创建 Node → 启动 → 等待退出信号 → 停止。
// 适用于 main() 中一行式调用（向后兼容老示例）。
// Deprecated: 新代码请使用 node.New().Start(...) + 手动信号处理。
func Start(opts ...StartOption) {
	n, err := New().Start(opts...)
	if err != nil {
		panic(fmt.Sprintf("node start failed: %v", err))
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	n.Stop()
}

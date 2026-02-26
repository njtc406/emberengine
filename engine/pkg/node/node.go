package node

import (
	"time"

	"github.com/njtc406/emberengine/engine/pkg/cluster"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/event"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/monitor"
	"github.com/njtc406/emberengine/engine/pkg/rpc/message/msgenvelope"
	"github.com/njtc406/emberengine/engine/pkg/services"
	"github.com/njtc406/emberengine/engine/pkg/utils/asynclib"
	"github.com/njtc406/emberengine/engine/pkg/utils/dedup"
	"github.com/njtc406/emberengine/engine/pkg/utils/pid"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
	"github.com/njtc406/emberengine/engine/pkg/utils/title"
	"github.com/njtc406/emberengine/engine/pkg/utils/translate"
	"github.com/njtc406/emberengine/engine/pkg/utils/version"
)

type Node struct {
	version   string
	confPath  string
	hooks     []HookFun
	extra     map[any]any
	startTime time.Time
}

func New() *Node {
	return &Node{}
}

func fixVersion(v string) string {
	if v == "" {
		return version.Version // 目前的框架版本
	}
	return v
}

func (n *Node) Start(opts ...StartOption) (*Node, error) {
	n.startTime = time.Now() // 使用真实时间
	param := StartParam{}
	for _, f := range opts {
		f(&param)
	}
	n.version = fixVersion(param.Version)
	n.confPath = param.ConfPath
	n.hooks = param.Hooks
	n.extra = param.Extra

	if param.Language > 0 {
		translate.SetLanguage(param.Language)
	}

	// 打印版本信息
	title.EchoTitle(n.version)

	// 初始化节点配置
	config.Init(n.confPath)

	// 初始化日志
	log.Init(config.Conf.SystemLogger, config.IsDebug())

	// 启动线程池
	asynclib.InitAntsPool(config.Conf.NodeConf.AntsPoolSize)

	// 启动timer
	// TODO 做成配置吧,有些精度要求不高的场景可以直接使用秒
	timingwheel.Start(config.Conf.NodeConf.TimingWheelConf.Interval, config.Conf.NodeConf.TimingWheelConf.WheelSize, log.SysLogger)

	// 初始化rpc监控
	monitor.GetRpcMonitor().Init(config.Conf.NodeConf.RpcMonitorConf)

	// 记录pid
	pid.RecordPID(config.Conf.NodeConf.PVPath, config.Conf.NodeConf.NodeId, config.Conf.NodeConf.NodeType)

	// 初始化rpc请求去重缓存器
	dedup.Init(config.Conf.NodeConf.DeDuplicatorConf)
	// 初始化等待队列,并启动监听
	monitor.GetRpcMonitor().Start()

	// 初始化集群设置
	cluster.GetCluster().Init()
	// 启动集群管理器
	cluster.GetCluster().Start()
	// 启动全局事件
	event.GetEventBus().Init(config.Conf.NodeConf.EventBusConf)

	// 执行钩子
	for _, f := range param.Hooks {
		f(param.Extra)
	}

	// TODO 服务的启动可能需要做成个命令来执行什么的,启动的时候先只启动节点,然后通过命令来启动服务
	// 这样就可以在热更的时候先启动一个新的节点,然后把老的节点停掉,然后再把新节点所有服务启动起来

	// 初始化服务
	services.Init()

	// 启动服务
	services.Start()

	return n, nil
}

func (n *Node) Stop() {
	defer pid.DeletePID(config.Conf.NodeConf.PVPath, config.Conf.NodeConf.NodeId, config.Conf.NodeConf.NodeType)
	log.SysLogger.Info("==================>>begin stop<<==================")
	log.SysLogger.Info("[1/6] Stopping all services...")
	services.StopAll()
	log.SysLogger.Info("[1/6] All services stopped")

	log.SysLogger.Info("[2/6] Closing cluster...")
	cluster.GetCluster().Close()
	log.SysLogger.Info("[2/6] Cluster closed")

	log.SysLogger.Info("[3/6] Stopping RPC monitor...")
	monitor.GetRpcMonitor().Stop()
	log.SysLogger.Info("[3/6] RPC monitor stopped")

	log.SysLogger.Info("[4/6] Stopping timing wheel...")
	timingwheel.Stop()
	log.SysLogger.Info("[4/6] Timing wheel stopped")

	log.SysLogger.Info("[5/6] Releasing async lib...")
	asynclib.Release() // 最后释放线程池,防止任务没有执行完就退出了
	log.SysLogger.Info("[5/6] Async lib released")

	log.SysLogger.Info("[6/6] Closing logger...")
	if config.IsDebug() {
		if dump := msgenvelope.DumpMetaPoolLeaks(20); dump != "" {
			log.SysLogger.Warn(dump)
		}
	}
	log.SysLogger.Info("server stopped, program exited...")
	log.Close()
	// 优雅退出
	title.GracefulExit(time.Since(n.startTime), n.version)
}

// Package node
// 模块名: 节点
// 功能描述: 用于提供程序入口
// 作者:  yr  2024/1/10 0010 23:43
// 最后更新:  yr  2024/1/10 0010 23:43
package node

import (
	"os"
	"os/signal"
	"syscall"
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

var (
	exitCh = make(chan os.Signal)
	ID     int32
	Type   string
)

func init() {
	// 注册退出信号
	signal.Notify(exitCh, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGINT)
}

func fixVersion(v string) string {
	if v == "" {
		return version.Version // 目前的框架版本
	}
	return v
}

type HookFun func(map[any]any)

type StartParam struct {
	Language translate.LanguageType // 语言
	Version  string
	ConfPath string
	Hooks    []HookFun
	Extra    map[any]any
}

type StartOption func(*StartParam)

func WithLanguage(language translate.LanguageType) StartOption {
	return func(p *StartParam) {
		p.Language = language
	}
}

func WithVersion(v string) StartOption {
	return func(p *StartParam) {
		p.Version = v
	}
}

func WithConfPath(confPath string) StartOption {
	return func(p *StartParam) {
		p.ConfPath = confPath
	}
}

func WithHooks(hooks ...HookFun) StartOption {
	return func(p *StartParam) {
		p.Hooks = hooks
	}
}

func WithExtra(extra map[any]any) StartOption {
	return func(p *StartParam) {
		p.Extra = extra
	}
}

func Start(opts ...StartOption) {
	startTime := time.Now()
	param := StartParam{}
	for _, f := range opts {
		f(&param)
	}
	param.Version = fixVersion(param.Version)

	if param.Language > 0 {
		translate.SetLanguage(param.Language)
	}

	// 打印版本信息
	title.EchoTitle(param.Version)

	// 初始化节点配置
	config.Init(param.ConfPath)

	// 初始化日志
	log.Init(config.Conf.SystemLogger, config.IsDebug())

	// 启动线程池
	asynclib.InitAntsPool(config.Conf.NodeConf.AntsPoolSize)

	// 启动timer
	// TODO 做成配置吧,有些精度要求不高的场景可以直接使用秒
	timingwheel.Start(config.Conf.NodeConf.TimingWheelConf.Interval, config.Conf.NodeConf.TimingWheelConf.WheelSize, log.SysLogger)

	// 记录pid
	pid.RecordPID(config.Conf.NodeConf.PVPath, ID, Type)
	defer pid.DeletePID(config.Conf.NodeConf.PVPath, ID, Type)

	// 初始化rpc请求去重缓存器
	dedup.Init(config.Conf.NodeConf.DeDuplicatorConf)
	// 初始化等待队列,并启动监听
	monitor.GetRpcMonitor().Init().Start()

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

	// 监听退出信号
	select {
	case sig := <-exitCh:
		log.SysLogger.Infof("-------------->>received the signal: %v", sig)
	}

	log.SysLogger.Info("==================>>begin stop<<==================")

	// 执行关闭流程
	shutdownSequence(startTime, param.Version)
}

// shutdownSequence 关闭序列 - 独立函数便于调试
// 在这个函数的任何地方都可以正常设置断点
func shutdownSequence(startTime time.Time, version string) {
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
	title.GracefulExit(time.Since(startTime), version)
}

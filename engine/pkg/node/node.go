// Package node
// 模块名: 节点
// 功能描述: 用于提供程序入口
// 作者:  yr  2024/1/10 0010 23:43
// 最后更新:  yr  2024/1/10 0010 23:43
package node

import (
	"github.com/njtc406/emberengine/engine/pkg/event"
	"github.com/njtc406/emberengine/engine/pkg/utils/dedup"
	"github.com/njtc406/emberengine/engine/pkg/utils/title"
	"github.com/njtc406/emberengine/engine/pkg/utils/translate"
	"github.com/njtc406/emberengine/engine/pkg/utils/version"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/njtc406/emberengine/engine/internal/monitor"
	"github.com/njtc406/emberengine/engine/pkg/cluster"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/services"
	"github.com/njtc406/emberengine/engine/pkg/utils/asynclib"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/pid"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
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
	Language translate.LanguageType
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

	// 启动timer(默认使用时间轮)
	timingwheel.Start(time.Millisecond*10, 100)

	// 记录pid
	pid.RecordPID(config.Conf.NodeConf.PVPath, ID, Type)
	defer pid.DeletePID(config.Conf.NodeConf.PVPath, ID, Type)

	// 初始化rpc请求去重缓存器
	dedup.Init(
		config.Conf.NodeConf.DeDuplicatorConf.DeDuplicatorType,
		dedup.WithCleanTTL(config.Conf.NodeConf.DeDuplicatorConf.DeDuplicatorCleanTTL),
		dedup.WithSize(config.Conf.NodeConf.DeDuplicatorConf.DeDuplicatorSize),
		dedup.WithTTL(config.Conf.NodeConf.DeDuplicatorConf.DeDuplicatorTTL),
	)
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

	log.SysLogger.Info("==================>>begin stop modules<<==================")
	timingwheel.Stop()
	services.StopAll()
	cluster.GetCluster().Close()
	monitor.GetRpcMonitor().Stop()
	asynclib.Release() // 最后释放线程池,防止任务没有执行完就退出了
	log.SysLogger.Info("server stopped, program exited...")
	log.Close()
	title.GracefulExit(time.Since(startTime), param.Version)
}

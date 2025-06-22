// Package node
// 模块名: 节点
// 功能描述: 用于提供程序入口
// 作者:  yr  2024/1/10 0010 23:43
// 最后更新:  yr  2024/1/10 0010 23:43
package node

import (
	"github.com/njtc406/emberengine/engine/pkg/event"
	"github.com/njtc406/emberengine/engine/pkg/utils/dedup"
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
	"github.com/njtc406/emberengine/engine/pkg/utils/version"
)

var (
	exitCh        = make(chan os.Signal)
	ID            int32
	Type          string
	nodeInitHooks []func() // 启动时需要执行的钩子
)

func init() {
	// 注册退出信号
	signal.Notify(exitCh, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGINT)
}

func SetStartHook(f ...func()) {
	nodeInitHooks = append(nodeInitHooks, f...)
}

func Start(v string, confPath string) {
	// 打印版本信息
	version.EchoVersion(v)

	// 初始化节点配置
	config.Init(confPath)

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
	dedup.GetRpcReqDuplicator().Init(config.Conf.NodeConf.RpcDuplicatorTTL)
	// 初始化等待队列,并启动监听
	monitor.GetRpcMonitor().Init().Start()

	// 初始化集群设置
	cluster.GetCluster().Init()
	// 启动集群管理器
	cluster.GetCluster().Start()
	// 启动全局事件
	event.GetEventBus().Init(config.Conf.NodeConf.EventBusConf)

	// 执行钩子
	for _, f := range nodeInitHooks {
		f()
	}

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
}

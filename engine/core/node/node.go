// Package node
// 模块名: 节点
// 功能描述: 用于提供程序入口
// 作者:  yr  2024/1/10 0010 23:43
// 最后更新:  yr  2024/1/10 0010 23:43
package node

import (
	"github.com/njtc406/emberengine/engine/cluster"
	"github.com/njtc406/emberengine/engine/config"
	"github.com/njtc406/emberengine/engine/monitor"
	"github.com/njtc406/emberengine/engine/profiler"
	"github.com/njtc406/emberengine/engine/services"
	"github.com/njtc406/emberengine/engine/utils/asynclib"
	"github.com/njtc406/emberengine/engine/utils/log"
	"github.com/njtc406/emberengine/engine/utils/pid"
	"github.com/njtc406/emberengine/engine/utils/timer"
	"github.com/njtc406/emberengine/engine/utils/version"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var (
	exitCh = make(chan os.Signal)
	ID     int32
	Type   string
	hooks  []func()
)

func init() {
	// 注册退出信号
	signal.Notify(exitCh, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGINT)
}

func SetStartHook(f ...func()) {
	hooks = append(hooks, f...)
}

func Start(v string, confPath string) {
	// 打印版本信息
	version.EchoVersion(v)
	// TODO: 这里后面如果加入集群,那么需要从集群中获取节点配置
	// 初始化节点配置
	config.Init(confPath)

	// 初始化日志
	log.Init(config.Conf.SystemLogger, config.IsDebug())

	// 启动线程池
	asynclib.InitAntsPool(config.Conf.NodeConf.AntsPoolSize)

	// 初始化等待队列,并启动监听
	monitor.GetRpcMonitor().Init().Start()

	// TODO 考虑把一些公共的组件都使用service去做, 这样就可以不用去考虑并发的一些问题
	// 比如cluster里面的一些组件

	// 初始化集群设置
	cluster.GetCluster().Init()
	// 启动集群管理器
	cluster.GetCluster().Start()

	// 记录pid
	pid.RecordPID(config.Conf.NodeConf.PVPath, ID, Type)
	defer pid.DeletePID(config.Conf.NodeConf.PVPath, ID, Type)

	// 启动timer
	timer.StartTimer(10*time.Millisecond, 1000000)

	// 执行钩子
	for _, f := range hooks {
		f()
	}

	// 初始化服务
	services.Init()

	// 启动服务
	services.Start()

	running := true
	pProfilerTicker := new(time.Ticker)
	defer pProfilerTicker.Stop()
	if config.Conf.NodeConf.ProfilerInterval > 0 {
		pProfilerTicker = time.NewTicker(config.Conf.NodeConf.ProfilerInterval)
	}

	for running {
		select {
		case sig := <-exitCh:
			log.SysLogger.Infof("-------------->>received the signal: %v", sig)
			running = false
		case <-pProfilerTicker.C:
			profiler.Report()
		}
	}
	log.SysLogger.Info("==================>>begin stop modules<<==================")
	timer.StopTimer() // 停止timer
	services.StopAll()
	cluster.GetCluster().Close()
	monitor.GetRpcMonitor().Stop()
	log.SysLogger.Info("server stopped, program exited...")
	log.Close()
}

// Package interfaces
// @Title  title
// @Description  desc
// @Author  pc  2024/11/4
// @Update  pc  2024/11/4
package interfaces

import (
	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/profiler"
)

// IService 服务接口
// 每个服务就是一个单独的协程
type IService interface {
	ILifecycle
	IIdentifiable
	IServiceHandler
	IEventChannel
	IProfiler
	ILogger
	IRpcHandler
}

// ILifecycle 服务生命周期
type ILifecycle interface {
	Init(src interface{}, serviceInitConf *config.ServiceInitConf, cfg interface{})
	Start() error
	Stop()
	OnInit() error
	OnStart() error
	OnStarted() error
	OnRelease()
}

type IServiceHandler interface {
	GetServiceCfg() interface{}
	GetMailbox() IMailbox
	IsPrivate() bool
	IsPrimarySecondaryMode() bool
	GetRpcHandler() IRpcHandler
}

type IIdentifiable interface {
	IServer
	INamed
	IActor
	IsClosed() bool // 服务是否已经关闭
}

type IProfiler interface {
	OpenProfiler()
	GetProfiler() *profiler.Profiler // TODO 需要将这个做成interface
}

type INamed interface {
	SetName(string)
	GetName() string
}

type IServer interface {
	IActor
	GetServerId() int32
}

// IActor 表示一个可寻址的 Actor 实体
type IActor interface {
	SetPid(pid *actor.PID)
	GetPid() *actor.PID
}

type ILogger interface {
	// GetLogger 获取日志记录器
	GetLogger() *log.Logger
	GetLoggerX() log.ILoggerX
}

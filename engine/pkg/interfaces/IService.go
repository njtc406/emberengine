// Package interfaces
// @Title  title
// @Description  desc
// @Author  pc  2024/11/4
// @Update  pc  2024/11/4
package interfaces

import (
	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/profiler"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
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
}

type IIdentifiable interface {
	IServer
	INamed
	IActor
	OnSetup(svc IService)
	IsClosed() bool // 服务是否已经关闭
}

type IProfiler interface {
	OpenProfiler()
	GetProfiler() *profiler.Profiler
}

type INamed interface {
	SetName(string)
	GetName() string
}

type IServer interface {
	IActor
	GetServerId() int32
}

type IActor interface {
	SetPid(pid *actor.PID)
	GetPid() *actor.PID
}

type ILogger interface {
	GetLogger() log.ILogger
}

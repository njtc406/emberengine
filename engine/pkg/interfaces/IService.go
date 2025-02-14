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
	"github.com/njtc406/emberengine/engine/pkg/utils/concurrent"
)

// IService 服务接口
// 每个服务就是一个单独的协程
type IService interface {
	concurrent.IConcurrent
	ILifecycle
	IIdentifiable
	IServiceHandler
	IEventChannel
	IProfiler
}

// ILifecycle 服务生命周期
type ILifecycle interface {
	Init(src interface{}, serviceInitConf *config.ServiceInitConf, cfg interface{})
	Start() error
	Stop()
	OnInit() error
	OnStart() error
	OnRelease()
}

type IServiceHandler interface {
	GetServiceCfg() interface{}
	GetMailbox() IMailbox
	IsPrivate() bool
}

type IIdentifiable interface {
	OnSetup(svc IService)
	SetName(string)
	GetName() string
	GetPid() *actor.PID
	GetServerId() int32
	IsClosed() bool // 服务是否已经关闭
}

type IProfiler interface {
	OpenProfiler()
	GetProfiler() *profiler.Profiler
}

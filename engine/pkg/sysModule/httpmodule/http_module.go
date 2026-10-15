// Package httpmodule
// 模块名: http服务
// 功能描述: 这是一个公用的http服务器
// 作者:  yr  2024/1/4 0004 23:41
// 最后更新:  yr  2024/1/4 0004 23:41
package httpmodule

import (
	"sync"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	"github.com/njtc406/emberengine/engine/pkg/core"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/utils/httpx"
	"github.com/njtc406/emberengine/engine/pkg/utils/httpx/router_center"
)

type HttpModule struct {
	core.Module
	running uint32

	systemMod string
	conf      *httpx.Conf
	server    *httpx.GinServer

	wg *sync.WaitGroup
}

func (hs *HttpModule) OnInit() error {
	// 替换验证器(这个东西之后再看用哪个版本)
	//*(binding.Validator.Engine().(*validator.Validate)) = *validate.Validator
	// 默认日志输出
	return hs.server.Init(hs.GetLogger(), hs.systemMod, hs.conf)
}

func (hs *HttpModule) OnStart() error {
	if !atomic.CompareAndSwapUint32(&hs.running, 0, 1) {
		return def.ErrServiceIsRunning
	}

	hs.server.Start()

	return nil
}

func (hs *HttpModule) OnRelease() {
	atomic.StoreUint32(&hs.running, 0)
	if hs.server == nil {
		return
	}
	hs.server.Stop()
	hs.server = nil
}

func (hs *HttpModule) WithBeforeServHook(hooks ...func()) *HttpModule {
	hs.server.WithBeforeServHook(hooks...)
	return hs
}

func (hs *HttpModule) WithInitHook(hooks ...func()) *HttpModule {
	hs.server.WithInitHook(hooks...)
	return hs
}

func (hs *HttpModule) WithRunHook(hooks ...func()) *HttpModule {
	hs.server.WithRunHook(hooks...)
	return hs
}

func (hs *HttpModule) WithStopHook(hooks ...func()) *HttpModule {
	hs.server.WithStopHook(hooks...)
	return hs
}

func (hs *HttpModule) SetRouter(router *router_center.GroupHandlerPool) *HttpModule {
	hs.server.SetRouter(router)
	return hs
}

func (hs *HttpModule) WithMiddleware(middleware ...gin.HandlerFunc) *HttpModule {
	hs.server.WithMiddleware(middleware...)
	return hs
}

// NewHttpModule 创建新的HTTP服务器
func NewHttpModule(conf *httpx.Conf, systemMod string) *HttpModule {
	return &HttpModule{
		conf:      conf,
		server:    httpx.NewGinServer(),
		wg:        new(sync.WaitGroup),
		systemMod: systemMod,
	}
}

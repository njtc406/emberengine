// Package pprofservice
// @Title  性能性分析服务
// @Description  性能性分析服务
// @Author  yr  2024/8/21 下午5:00
// @Update  yr  2024/8/21 下午5:00
package pprofservice

import (
	"github.com/gin-gonic/gin"
	sysConfig "github.com/njtc406/emberengine/engine/config"
	"github.com/njtc406/emberengine/engine/core"
	"github.com/njtc406/emberengine/engine/inf"
	"github.com/njtc406/emberengine/engine/services"
	"github.com/njtc406/emberengine/engine/sysModule/httpmodule"
	"github.com/njtc406/emberengine/engine/sysModule/httpmodule/router_center"
	"github.com/njtc406/emberengine/engine/sysService/pprofservice/config"
	"github.com/njtc406/emberengine/engine/utils/log"
	"net/http"
	_ "net/http/pprof"
)

func init() {
	services.SetService("PprofService", func() inf.IService { return &PprofService{} })
	sysConfig.RegisterServiceConf(&sysConfig.ServiceConfig{
		ServiceName:   "PprofService",
		ConfName:      "pprof",
		ConfType:      "yaml",
		ConfPath:      "",
		CfgCreator:    func() interface{} { return &config.PprofConf{} },
		DefaultSetFun: config.SetPprofConfDefault,
		OnChangeFun: func() {
			// TODO 配置变更回调
		},
	})
}

type PprofService struct {
	core.Service

	httpModule *httpmodule.HttpModule
}

func (ps *PprofService) getConf() *config.PprofConf {
	return ps.GetServiceCfg().(*config.PprofConf)
}

func (ps *PprofService) OnInit() error {
	ps.httpModule = httpmodule.NewHttpModule(ps.getConf().PprofConf, log.SysLogger, sysConfig.GetStatus())
	ps.httpModule.SetRouter(ps.initRouter())
	_, err := ps.AddModule(ps.httpModule)
	if err != nil {
		return err
	}
	return nil
}

func (ps *PprofService) initRouter() *router_center.GroupHandlerPool {
	router := router_center.NewGroupHandlerPool()
	router.RegisterGroupHandler("", ps.routerHandler)
	return router
}

func (ps *PprofService) routerHandler(r *gin.RouterGroup) {
	r.GET("/debug/pprof", gin.WrapH(http.DefaultServeMux))
	r.GET("/debug/pprof/*pprof", gin.WrapH(http.DefaultServeMux))
}

func (ps *PprofService) OnStart() error {
	if err := ps.httpModule.OnStart(); err != nil {
		log.SysLogger.Panic(err)
	}
	return nil
}

func (ps *PprofService) OnRelease() {
	ps.ReleaseAllChildModule()
}

// Package services
// @Title  服务管理
// @Description  所有的服务都需要注册到这里,然后通过配置文件进行启动
// @Author  yr  2024/7/22 下午2:30
// @Update  yr  2024/7/22 下午2:30
package services

import (
	"sync"

	"github.com/njtc406/emberengine/engine/pkg/config"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
)

var (
	lock        sync.RWMutex
	serviceMap  map[string]func() inf.IService // 节点上拥有的服务
	runServices []inf.IService                 // 运行中的服务
)

func init() {
	serviceMap = make(map[string]func() inf.IService)
}

// SetService 注册主服务
func SetService(name string, creator func() inf.IService) {
	lock.Lock()
	serviceMap[name] = creator
	lock.Unlock()
}

func Init() {
	lock.RLock()
	defer lock.RUnlock()

	for _, initConf := range config.Conf.ServiceConf.StartServices {
		if creator, ok := serviceMap[initConf.ClassName]; ok {
			log.SysLogger.Infof("Init Service: %s", initConf.ClassName)
			svc := creator()
			serviceName := initConf.ClassName
			if initConf.ServiceName != "" {
				serviceName = initConf.ServiceName
			}
			svc.SetName(serviceName)
			svc.OnSetup(svc)
			var cfg interface{}
			//log.SysLogger.Debugf("service[%s] conf: %+v", initConf.ServiceName, config.GetServiceConf(initConf.ServiceName))
			if serviceConf, ok := config.Conf.ServiceConf.ServicesConfMap[serviceName]; ok {
				cfg = serviceConf.Cfg
			}
			svc.Init(svc, initConf, cfg)
			runServices = append(runServices, svc)
		} else {
			// 包中没有引入这个服务,但是配置中配置了启动这个服务
			log.SysLogger.Errorf("Service[%s] is configured to start but not imported in the package. Please check service dependencies or remove it from configuration", initConf.ClassName)
		}
	}
}

func Start() {
	lock.RLock()
	defer lock.RUnlock()
	for _, svc := range runServices {
		log.SysLogger.Infof("Start Service: %s", svc.GetName())
		if err := svc.Start(); err != nil {
			log.SysLogger.Errorf("Start Service: %s failed, err: %s", svc.GetName(), err)
		}
	}
	log.SysLogger.Infof("=============服务启动完成===================")
}

func StopAll() {
	lock.RLock()
	defer lock.RUnlock()
	for i := len(runServices) - 1; i >= 0; i-- {
		log.SysLogger.Infof("Stop Service: %s", runServices[i].GetName())
		runServices[i].Stop()
	}
}

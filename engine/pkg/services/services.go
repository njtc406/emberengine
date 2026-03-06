// Package services
// @Title  服务管理
// @Description  所有的服务都需要注册到这里,然后通过配置文件进行启动
// @Author  yr  2024/7/22 下午2:30
// @Update  yr  2024/7/22 下午2:30
package services

import (
	"fmt"
	"sync"

	"github.com/njtc406/emberengine/engine/pkg/cluster"
	"github.com/njtc406/emberengine/engine/pkg/cluster/endpoints"
	"github.com/njtc406/emberengine/engine/pkg/config"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/profiler"
	"github.com/njtc406/emberengine/engine/pkg/router"
)

// ===== 全局工厂注册表（保留为包级变量，init() 阶段注册，运行时只读） =====
var (
	lock       sync.RWMutex
	serviceMap = make(map[string]func() inf.IService)
)

// SetService 注册服务工厂（供 init() 阶段调用）。
func SetService(name string, builder func() inf.IService) {
	lock.Lock()
	serviceMap[name] = builder
	lock.Unlock()
}

// GetServiceFactory 返回已注册的服务工厂函数（供 ServiceManager 查询）。
func GetServiceFactory(name string) (func() inf.IService, bool) {
	lock.RLock()
	defer lock.RUnlock()
	f, ok := serviceMap[name]
	return f, ok
}

// ===== ServiceManager: per-Node 的运行时服务管理 =====

// ServiceManager 管理单个 Node 的运行时服务实例。
type ServiceManager struct {
	log.ILoggerX // 持有 ILoggerX
	runServices  []inf.IService
	daemon       *daemon
	cluster      *cluster.Cluster
	endpoints    *endpoints.EndpointManager
	profilerReg  *profiler.Registry
	router       *router.Router
	nodeCtx      inf.INodeContext
}

type runtimeDepsAware interface {
	SetRuntimeDeps(c *cluster.Cluster, em *endpoints.EndpointManager, pr *profiler.Registry, rt *router.Router)
}

type nodeContextAware interface {
	SetNodeContext(ctx inf.INodeContext)
}

// NewServiceManager 创建 ServiceManager。
func NewServiceManager(logger log.ILoggerX) *ServiceManager {
	return &ServiceManager{
		ILoggerX: logger,
	}
}

func (sm *ServiceManager) SetRuntimeDeps(c *cluster.Cluster, em *endpoints.EndpointManager, pr *profiler.Registry, rt *router.Router) {
	sm.cluster = c
	sm.endpoints = em
	sm.profilerReg = pr
	sm.router = rt
}

func (sm *ServiceManager) SetNodeContext(ctx inf.INodeContext) {
	sm.nodeCtx = ctx
}

// Init 根据配置创建并初始化所有服务。
// 任一服务初始化失败将立即返回错误，由上层决定后续处理。
func (sm *ServiceManager) Init(serviceConf *config.ServiceConf) error {
	type initEntry struct {
		initConf *config.ServiceInitConf
		builder  func() inf.IService
	}

	entries := make([]initEntry, 0, len(serviceConf.StartServices))
	lock.RLock()
	for _, initConf := range serviceConf.StartServices {
		builder, ok := serviceMap[initConf.ClassName]
		if !ok {
			lock.RUnlock()
			sm.WithField("service", initConf.ClassName).Error("Service is configured to start but not imported in the package. Please check service dependencies or remove it from configuration")
			return fmt.Errorf("service[%s] is configured to start but not registered", initConf.ClassName)
		}
		entries = append(entries, initEntry{initConf: initConf, builder: builder})
	}
	lock.RUnlock()

	for _, entry := range entries {
		initConf := entry.initConf
		sm.WithField("service", initConf.ClassName).Info("Init Service")
		svc := entry.builder()
		serviceName := initConf.ClassName
		if initConf.ServiceName != "" {
			serviceName = initConf.ServiceName
		}
		svc.SetName(serviceName)
		var cfg interface{}
		if serviceCfg, ok := serviceConf.ServicesConfMap[serviceName]; ok {
			cfg = serviceCfg.Cfg
		}
		if depAware, ok := svc.(runtimeDepsAware); ok {
			depAware.SetRuntimeDeps(sm.cluster, sm.endpoints, sm.profilerReg, sm.router)
		}
		if ctxAware, ok := svc.(nodeContextAware); ok {
			ctxAware.SetNodeContext(sm.nodeCtx)
		}
		if err := svc.Init(svc, initConf, cfg); err != nil {
			sm.WithField("service", serviceName).Errorf("Init Service failed, err: %v", err)
			return fmt.Errorf("init service[%s] failed: %w", serviceName, err)
		}
		sm.runServices = append(sm.runServices, svc)
	}

	return nil
}

// Start 启动所有已初始化的服务。
// 任一服务启动失败将立即中断，并回滚已启动服务。
func (sm *ServiceManager) Start() error {
	started := make([]inf.IService, 0, len(sm.runServices))
	for _, svc := range sm.runServices {
		sm.WithField("service", svc.GetName()).Info("Start Service")
		if err := svc.Start(); err != nil {
			sm.WithField("service", svc.GetName()).Errorf("Start Service failed, err: %v", err)
			for i := len(started) - 1; i >= 0; i-- {
				sm.WithField("service", started[i].GetName()).Info("Rollback Stop Service")
				started[i].Stop()
			}
			return fmt.Errorf("start service[%s] failed: %w", svc.GetName(), err)
		}
		started = append(started, svc)
	}
	sm.Info("=============服务启动完成===================")
	return nil
}

// StopAll 倒序停止所有服务。
func (sm *ServiceManager) StopAll() {
	for i := len(sm.runServices) - 1; i >= 0; i-- {
		sm.WithField("service", sm.runServices[i].GetName()).Info("Stop Service")
		sm.runServices[i].Stop()
	}
}

// GetDaemon 返回守护服务（如果需要）。
func (sm *ServiceManager) GetDaemon() *daemon {
	return sm.daemon
}

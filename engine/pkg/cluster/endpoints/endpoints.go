// Package endpoints
// @Title  请填写文件名称（需要改）
// @Description  请填写文件描述（需要改）
// @Author  yr  2024/8/29 下午6:24
// @Update  yr  2024/8/29 下午6:24
package endpoints

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/njtc406/emberengine/engine/pkg/actor"
	disc "github.com/njtc406/emberengine/engine/pkg/cluster/discovery"
	"github.com/njtc406/emberengine/engine/pkg/cluster/endpoints/repository"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/rpc/client"
	"github.com/njtc406/emberengine/engine/pkg/rpc/message/msgbus"
	"github.com/njtc406/emberengine/engine/pkg/rpc/remote"
	remotehandler "github.com/njtc406/emberengine/engine/pkg/rpc/remote/handler"
	"github.com/njtc406/emberengine/engine/pkg/utils/xcontext"
	"go.etcd.io/etcd/api/v3/mvccpb"
)

type EndpointManager struct {
	log.ILoggerX // 持有 ILoggerX

	eventProcessor *event.Processor
	eventHandler   *event.Handler

	nodeUid       string
	isClusterMode bool
	remotes       map[string]*remote.Remote // 远程服务监听器
	stopped       atomic.Bool               // 是否已停止
	repository    *repository.Repository    // 服务存储仓库
	senderMgr     *client.SenderManager
}

// NewEndpointManager 创建新的 EndpointManager 实例（Phase 2 per-Node 模式推荐使用）。
func NewEndpointManager() *EndpointManager {
	return &EndpointManager{}
}
func (em *EndpointManager) Init(eventProcessor *event.Processor, clusterConf *config.ClusterConf, logger log.ILoggerX, senderMgr *client.SenderManager) (*EndpointManager, error) {
	return em.InitWithDeps(eventProcessor, clusterConf, logger, senderMgr, nil, nil, nil)
}

func (em *EndpointManager) InitWithDeps(eventProcessor *event.Processor, clusterConf *config.ClusterConf, logger log.ILoggerX, senderMgr *client.SenderManager, rpcHandler *remotehandler.Handler, natsConf *config.NatsConf, busFactory *msgbus.MessageBusFactory) (*EndpointManager, error) {
	em.ILoggerX = logger
	em.senderMgr = senderMgr
	em.nodeUid = uuid.NewString()
	em.remotes = make(map[string]*remote.Remote)
	for _, cfg := range clusterConf.RPCServers {
		rt, err := remote.NewRemote().Init(cfg, em, em.ILoggerX, rpcHandler, natsConf)
		if err != nil {
			return nil, fmt.Errorf("init remote server[%s] failed: %w", cfg.Type, err)
		}
		em.remotes[cfg.Type] = rt
	}

	em.eventProcessor = eventProcessor

	// 事件管理
	em.eventHandler = event.NewTriggerHandler()
	em.eventHandler.Init(em.eventProcessor)

	em.repository = repository.NewRepository(busFactory)

	return em, nil
}

func (em *EndpointManager) Start() error {
	em.repository.Start()
	// 启动rpc监听服务器
	for _, rt := range em.remotes {
		rt.Serve(em.nodeUid)
	}

	// 新增、修改服务事件
	if err := event.RegisterHandler(em.eventHandler, event.SysEventETCDPut, "service_update", em.updateServiceInfo); err != nil {
		return fmt.Errorf("register service_update error: %w", err)
	}
	// 删除服务事件
	if err := event.RegisterHandler(em.eventHandler, event.SysEventETCDDel, "service_update", em.removeServiceInfo); err != nil {
		return fmt.Errorf("register service_update error: %w", err)
	}
	return nil
}

func (em *EndpointManager) Stop() {
	em.stopped.Store(true)
	for _, rt := range em.remotes {
		rt.Close()
	}
	em.repository.Stop()
	em.Debugf("endpoints manager stopped")
}

func (em *EndpointManager) SetClusterMode(isClusterMode bool) {
	em.isClusterMode = isClusterMode
}

func (em *EndpointManager) GetNodeUid() string {
	return em.nodeUid
}

// updateServiceInfo 更新远程服务信息事件
func (em *EndpointManager) updateServiceInfo(ctx context.Context, kv *mvccpb.KeyValue) error {
	if kv == nil || kv.Key == nil {
		em.WithContext(ctx).Errorf("update service error: key is nil")
		return fmt.Errorf("key is nil")
	}

	pid, status, visibility, err := disc.UnmarshalServiceEntry(kv.Value)
	if err != nil {
		em.WithContext(ctx).Errorf("unmarshal pid error: %v", err)
		return fmt.Errorf("unmarshal pid error: %v", err)
	}

	if pid.GetNodeUid() == em.nodeUid {
		em.WithContext(ctx).Debugf("endpointmgr ignore local service -> remote: %s local: %s  pid:%s", pid.GetNodeUid(), em.nodeUid, pid.String())
		// 本地服务,忽略
		return nil
	}
	em.WithContext(ctx).Infof("endpointmgr add remote service: %s, key: %s", pid.String(), string(kv.Key))
	em.repository.AddWithMeta(string(kv.Key), client.NewDispatcher(em.senderMgr, pid, nil), status, visibility)
	return nil
}

// removeServiceInfo 删除远程服务信息事件
func (em *EndpointManager) removeServiceInfo(ctx context.Context, kv *mvccpb.KeyValue) error {
	if kv == nil || kv.Key == nil {
		em.WithContext(ctx).Errorf("remove service error: key is nil")
		return fmt.Errorf("key is nil")
	}
	em.Infof("endpointmgr remove remote service: %s", string(kv.Key))
	em.repository.Remove(string(kv.Key))
	return nil
}

// AddService 添加本地服务到服务发现中
func (em *EndpointManager) AddService(svc inf.IService) {
	pid := svc.GetPid()
	if pid == nil {
		em.Errorf("add service error: pid is nil")
		return
	}

	defer func() {
		em.Debugf("add local service: %s, pid: %v", svc.GetName(), svc.GetPid().String())
	}()

	// 先加入本地集群
	em.repository.AddWithMeta("", client.NewDispatcher(em.senderMgr, pid, svc.GetMailbox()), svc.GetStatus(), svc.GetVisibility())

	// cluster visibility 控制服务发现发布；主从模式即使是 node visibility，
	// 也需要启动主从 watcher 参与独立的主从选举通道。
	if (svc.GetVisibility() != def.ServiceVisibilityCluster && !svc.IsPrimarySecondaryMode()) || !em.isClusterMode {
		return
	}

	// em.Debugf("add service to cluster, pid: %v", pid.String())

	// 这是同步执行的
	// 将服务信息发布到集群
	em.eventProcessor.Trigger(xcontext.New(nil), event.SysEventServiceReg, svc)
}

func (em *EndpointManager) ServiceReady(svc inf.IService) {
	pid := svc.GetPid()
	if pid == nil {
		em.Errorf("service ready error: pid is nil")
		return
	}
	em.repository.UpdateStatus(pid.GetServiceUid(), svc.GetStatus())
	if (svc.GetVisibility() != def.ServiceVisibilityCluster && !svc.IsPrimarySecondaryMode()) || !em.isClusterMode {
		return
	}
	em.eventProcessor.Trigger(xcontext.New(nil), event.SysEventServiceReg, svc)
}

func (em *EndpointManager) RemoveService(svc inf.IService) {
	pid := svc.GetPid()
	em.repository.Remove(pid.GetServiceUid())

	if (svc.GetVisibility() != def.ServiceVisibilityCluster && !svc.IsPrimarySecondaryMode()) || !em.isClusterMode || em.eventProcessor == nil {
		return
	}

	// em.Debugf("remove service from cluster, pid: %v", pid.String())

	// 通知集群服务下线
	em.eventProcessor.Trigger(xcontext.New(nil), event.SysEventServiceDis, svc.GetPid())
}

func (em *EndpointManager) ToNodeService(svc inf.IService) {
	if svc == nil || svc.GetPid() == nil {
		return
	}
	em.repository.UpdateVisibility(svc.GetPid().GetServiceUid(), def.ServiceVisibilityNode)
	if svc.IsPrimarySecondaryMode() {
		// 主从 watcher 与服务发现可见性解耦。动态降级为 node 时，不能停止
		// 主从 watcher，否则会释放 master lease 并退出主从小集群。
		return
	}
	if !em.isClusterMode || em.eventProcessor == nil {
		return
	}
	em.eventProcessor.Trigger(xcontext.New(nil), event.SysEventServiceDis, svc.GetPid())
}

func (em *EndpointManager) GetRepository() *repository.Repository {
	return em.repository
}

func (em *EndpointManager) GetDispatcher(pid *actor.PID) inf.IRpcDispatcher {
	if em.repository == nil || pid == nil {
		return nil
	}
	if pid.GetNodeUid() == em.nodeUid {
		cli := em.repository.SelectByServiceUid(pid.GetServiceUid())
		if cli == nil || !em.repository.IsRemoteCallable(pid.GetServiceUid()) {
			return nil
		}
		return cli
	}
	cli := em.repository.SelectByServiceUid(pid.GetServiceUid())
	if cli == nil {
		// 有一种情况下可能是空的,就是调用者是私有服务,那么此时就单独创建一个,放入临时仓库
		return em.repository.AddTmp(client.NewTmpDispatcher(em.senderMgr, pid, nil))
	}
	return cli
}

func (em *EndpointManager) CreatePid(partition int32, serviceId, serviceType, serviceName string, version int64, rpcType string) *actor.PID {
	rt, ok := em.remotes[rpcType]
	if !ok {
		return actor.NewPID("", em.nodeUid, partition, serviceId, serviceType, serviceName, version, "")
	} else {
		return actor.NewPID(rt.GetAddress(), em.nodeUid, partition, serviceId, serviceType, serviceName, version, rpcType)
	}
}

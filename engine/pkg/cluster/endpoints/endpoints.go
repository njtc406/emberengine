// Package endpoints
// @Title  请填写文件名称（需要改）
// @Description  请填写文件描述（需要改）
// @Author  yr  2024/8/29 下午6:24
// @Update  yr  2024/8/29 下午6:24
package endpoints

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/cluster/endpoints/repository"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/rpc/client"
	"github.com/njtc406/emberengine/engine/pkg/rpc/remote"
	"github.com/njtc406/emberengine/engine/pkg/utils/xcontext"
	"go.etcd.io/etcd/api/v3/mvccpb"
	"google.golang.org/protobuf/encoding/protojson"
)

var endMgr = &EndpointManager{}

type EndpointManager struct {
	eventProcessor *event.Processor
	eventHandler   *event.Handler

	nodeUid       string
	isClusterMode bool
	remotes       map[string]*remote.Remote // 远程服务监听器
	stopped       bool                      // 是否已停止
	repository    *repository.Repository    // 服务存储仓库
}

func GetEndpointManager() *EndpointManager {
	return endMgr
}

func (em *EndpointManager) Init(eventProcessor *event.Processor) *EndpointManager {
	em.nodeUid = uuid.NewString()
	em.remotes = make(map[string]*remote.Remote)
	for _, cfg := range config.Conf.ClusterConf.RPCServers {
		em.remotes[cfg.Type] = remote.NewRemote().Init(cfg, em)
	}

	em.eventProcessor = eventProcessor

	// 事件管理
	em.eventHandler = event.NewTriggerHandler()
	em.eventHandler.Init(em.eventProcessor)

	em.repository = repository.NewRepository()

	return em
}

func (em *EndpointManager) Start() {
	em.repository.Start()
	// 启动rpc监听服务器
	for _, rt := range em.remotes {
		rt.Serve(em.nodeUid)
	}

	// 新增、修改服务事件
	if err := event.RegisterHandler(em.eventHandler, event.SysEventETCDPut, "service_update", em.updateServiceInfo); err != nil {
		log.SysLogger.Panicf("register service_update error: %v", err)
	}
	// 删除服务事件
	if err := event.RegisterHandler(em.eventHandler, event.SysEventETCDDel, "service_update", em.removeServiceInfo); err != nil {
		log.SysLogger.Panicf("register service_update error: %v", err)
	}
}

func (em *EndpointManager) Stop() {
	em.stopped = true
	for _, rt := range em.remotes {
		rt.Close()
	}
	em.repository.Stop()
	client.Close() // 关闭所有连接
	log.SysLogger.Debugf("endpoints manager stopped")
}

func (em *EndpointManager) SetClusterMode(isClusterMode bool) {
	em.isClusterMode = isClusterMode
}

// updateServiceInfo 更新远程服务信息事件
func (em *EndpointManager) updateServiceInfo(ctx context.Context, kv *mvccpb.KeyValue) error {
	if kv == nil || kv.Key == nil {
		log.SysLogger.WithContext(ctx).Errorf("update service error: key is nil")
		return fmt.Errorf("key is nil")
	}

	var pid actor.PID
	if err := protojson.Unmarshal(kv.Value, &pid); err != nil {
		log.SysLogger.WithContext(ctx).Errorf("unmarshal pid error: %v", err)
		return fmt.Errorf("unmarshal pid error: %v", err)
	}

	if pid.GetNodeUid() == em.nodeUid {
		log.SysLogger.WithContext(ctx).Debugf("endpointmgr ignore local service -> remote: %s local: %s  pid:%s", pid.GetNodeUid(), em.nodeUid, pid.String())
		// 本地服务,忽略
		return fmt.Errorf("ignore local service")
	}
	log.SysLogger.WithContext(ctx).Infof("endpointmgr add remote service: %s, key: %s", pid.String(), string(kv.Key))
	em.repository.Add(string(kv.Key), client.NewDispatcher(&pid, nil))
	return nil
}

// removeServiceInfo 删除远程服务信息事件
func (em *EndpointManager) removeServiceInfo(ctx context.Context, kv *mvccpb.KeyValue) error {
	if kv == nil || kv.Key == nil {
		log.SysLogger.WithContext(ctx).Errorf("remove service error: key is nil")
		return fmt.Errorf("key is nil")
	}
	log.SysLogger.Infof("endpointmgr remove remote service: %s", string(kv.Key))
	em.repository.Remove(string(kv.Key))
	return nil
}

// AddService 添加本地服务到服务发现中
func (em *EndpointManager) AddService(svc inf.IService) {
	pid := svc.GetPid()
	if pid == nil {
		log.SysLogger.Errorf("add service error: pid is nil")
		return
	}

	defer func() {
		log.SysLogger.Debugf("add local service: %s, pid: %v", svc.GetName(), svc.GetPid().String())
	}()

	// 先加入本地集群
	em.repository.Add("", client.NewDispatcher(pid, svc.GetMailbox()))

	// 私有服务不发布,没有开启集群也不发布
	if svc.IsPrivate() || !em.isClusterMode {
		return
	}

	//log.SysLogger.Debugf("add service to cluster ,pid: %v", pid.String())

	// 这是同步执行的
	// 将服务信息发布到集群
	em.eventProcessor.Trigger(xcontext.New(nil), event.SysEventServiceReg, svc)

	return
}

func (em *EndpointManager) RemoveService(svc inf.IService) {
	pid := svc.GetPid()
	em.repository.Remove(pid.GetServiceUid())

	if svc.IsPrivate() {
		return
	}

	//log.SysLogger.Debugf("add service to cluster ,pid: %v", pid.String())

	// 通知集群服务下线
	em.eventProcessor.Trigger(xcontext.New(nil), event.SysEventServiceDis, svc.GetPid())
}

func (em *EndpointManager) ToPrivateService(svc inf.IService) {
	em.eventProcessor.Trigger(xcontext.New(nil), event.SysEventServiceDis, svc.GetPid())
}

func (em *EndpointManager) GetRepository() *repository.Repository {
	return em.repository
}

func (em *EndpointManager) GetDispatcher(pid *actor.PID) inf.IRpcDispatcher {
	cli := em.repository.SelectByServiceUid(pid.GetServiceUid())
	if cli == nil {
		// 有一种情况下可能是空的,就是调用者是私有服务,那么此时就单独创建一个,放入临时仓库
		return em.repository.AddTmp(client.NewTmpDispatcher(pid, nil))
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

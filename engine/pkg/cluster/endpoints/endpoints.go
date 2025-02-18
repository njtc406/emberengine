// Package endpoints
// @Title  请填写文件名称（需要改）
// @Description  请填写文件描述（需要改）
// @Author  yr  2024/8/29 下午6:24
// @Update  yr  2024/8/29 下午6:24
package endpoints

import (
	"github.com/google/uuid"
	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/cluster/endpoints/client"
	"github.com/njtc406/emberengine/engine/pkg/cluster/endpoints/remote"
	"github.com/njtc406/emberengine/engine/pkg/cluster/endpoints/repository"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"go.etcd.io/etcd/api/v3/mvccpb"
	"google.golang.org/protobuf/encoding/protojson"
)

var endMgr = &EndpointManager{}

type EndpointManager struct {
	inf.IEventProcessor
	inf.IEventHandler

	nodeUid    string
	remotes    map[string]*remote.Remote // 远程服务监听器
	stopped    bool                      // 是否已停止
	repository *repository.Repository    // 服务存储仓库
}

func GetEndpointManager() *EndpointManager {
	return endMgr
}

func (em *EndpointManager) Init(eventProcessor inf.IEventProcessor) *EndpointManager {
	em.nodeUid = uuid.NewString()
	em.remotes = make(map[string]*remote.Remote)
	for _, cfg := range config.Conf.ClusterConf.RPCServers {
		em.remotes[cfg.Type] = remote.NewRemote().Init(cfg, em)
	}

	em.IEventProcessor = eventProcessor

	// 事件管理
	em.IEventProcessor = eventProcessor
	em.IEventHandler = event.NewHandler()
	em.IEventHandler.Init(em.IEventProcessor)

	em.repository = repository.NewRepository()

	return em
}

func (em *EndpointManager) Start() {
	em.repository.Start()
	// 启动rpc监听服务器
	for _, rt := range em.remotes {
		rt.Serve()
	}

	// 新增、修改服务事件
	em.IEventProcessor.RegEventReceiverFunc(event.SysEventETCDPut, em.IEventHandler, em.updateServiceInfo)
	// 删除服务事件
	em.IEventProcessor.RegEventReceiverFunc(event.SysEventETCDDel, em.IEventHandler, em.removeServiceInfo)
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

// updateServiceInfo 更新远程服务信息事件
func (em *EndpointManager) updateServiceInfo(e inf.IEvent) {
	//log.SysLogger.Debugf("endpoints receive update service event: %+v", e)
	ev := e.(*event.Event)
	kv := ev.Data.(*mvccpb.KeyValue)
	if kv.Value != nil {
		var pid actor.PID
		if err := protojson.Unmarshal(kv.Value, &pid); err != nil {
			log.SysLogger.Errorf("unmarshal pid error: %v", err)
			return
		}

		if pid.GetNodeUid() == em.nodeUid {
			//log.SysLogger.Debugf("endpointmgr ignore -> remote: %s local: %s  pid:%s", pid.GetNodeUid(), em.nodeUid, pid.String())
			// 本地服务,忽略
			return
		}
		//log.SysLogger.Debugf(">>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>endpointmgr add remote service: %s", pid.String())
		em.repository.Add(client.NewDispatcher(&pid, nil))
	}
}

type delKey struct {
	Key string
}

// removeServiceInfo 删除远程服务信息事件
func (em *EndpointManager) removeServiceInfo(e inf.IEvent) {
	ev := e.(*event.Event)
	kv := ev.Data.(*mvccpb.KeyValue)
	if kv.Key != nil {
		em.repository.Remove(string(kv.Key))
	} else {
		log.SysLogger.Errorf("remove service error: key is nil")
	}
}

// AddService 添加本地服务到服务发现中
func (em *EndpointManager) AddService(svc inf.IService) {
	//log.SysLogger.Debugf("add local service: %s, pid: %v", pid.String(), mailbox)
	pid := svc.GetPid()
	if pid == nil {
		log.SysLogger.Errorf("add service error: pid is nil")
		return
	}
	em.repository.Add(client.NewDispatcher(pid, svc.GetMailbox()))

	// 私有服务不发布
	if svc.IsPrivate() {
		return
	}

	//log.SysLogger.Debugf("add service to cluster ,pid: %v", pid.String())

	// 将服务信息发布到集群
	ev := event.NewEvent()
	ev.Type = event.SysEventServiceReg
	ev.Data = pid
	em.IEventProcessor.EventHandler(ev)

	return
}

func (em *EndpointManager) RemoveService(svc inf.IService) {
	pid := svc.GetPid()
	em.repository.Remove(pid.GetServiceUid())

	if svc.IsPrivate() {
		return
	}

	//log.SysLogger.Debugf("add service to cluster ,pid: %v", pid.String())

	// 将服务信息发布到集群
	ev := event.NewEvent()
	ev.Type = event.SysEventServiceDis
	ev.Data = pid
	em.IEventProcessor.EventHandler(ev)
}

// UpdateService 更新服务信息(服务信息发生变化后调用,比如version发生变化等待)
func (em *EndpointManager) UpdateService(svc inf.IService) {
	pid := svc.GetPid()
	em.repository.Add(client.NewDispatcher(pid, svc.GetMailbox()))
	if svc.IsPrivate() {
		return
	}

	//log.SysLogger.Debugf("add service to cluster ,pid: %v", pid.String())

	// 将服务信息发布到集群
	ev := event.NewEvent()
	ev.Type = event.SysEventServiceUpdate
	ev.Data = pid
	em.IEventProcessor.EventHandler(ev)
}

func (em *EndpointManager) GetDispatcher(pid *actor.PID) inf.IRpcDispatcher {
	cli := em.repository.SelectByServiceUid(pid.GetServiceUid())
	if cli == nil {
		// 有一种情况下可能是空的,就是调用者是私有服务,那么此时就单独创建一个,放入临时仓库
		return em.repository.AddTmp(client.NewTmpDispatcher(pid, nil))
	}
	return cli
}

func (em *EndpointManager) CreatePid(serverId int32, serviceId, serviceType, serviceName string, version int64, rpcType string) *actor.PID {
	rt, ok := em.remotes[rpcType]
	if !ok {
		log.SysLogger.Errorf("not found rpc type: %s", rpcType)
		return nil
	}

	return actor.NewPID(rt.GetAddress(), em.nodeUid, serverId, serviceId, serviceType, serviceName, version, rpcType)
}

// Package cluster
// @Title  集群模块
// @Description  desc
// @Author  pc  2024/11/5
// @Update  pc  2024/11/5
package cluster

import (
	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/cluster/discovery"
	"github.com/njtc406/emberengine/engine/pkg/cluster/endpoints"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
)

var cluster Cluster

func GetCluster() *Cluster {
	return &cluster
}

type Cluster struct {
	closed chan struct{}

	// 服务发现
	discovery inf.IDiscovery

	// 节点列表
	endpoints *endpoints.EndpointManager

	// 事件
	eventProcessor inf.IEventProcessor
	eventChannel   chan inf.IEvent
}

func (c *Cluster) Init() {
	c.closed = make(chan struct{})
	c.eventChannel = make(chan inf.IEvent, 1024)
	c.eventProcessor = event.NewProcessor()
	c.eventProcessor.Init(c)

	c.endpoints = endpoints.GetEndpointManager().Init(c.eventProcessor)

	c.discovery = discovery.CreateDiscovery(config.Conf.ClusterConf.DiscoveryType)
	if c.discovery == nil {
		return
	}
	if err := c.discovery.Init(c.eventProcessor, config.Conf.ClusterConf); err != nil {
		log.SysLogger.Fatalf("init discovery error:%v", err)
	}
}

func (c *Cluster) Start() {
	c.discovery.Start()
	c.endpoints.Start()
	go c.run()
}

func (c *Cluster) Close() {
	close(c.closed)
	c.endpoints.Stop()
	c.discovery.Close()
}

func (c *Cluster) PushEvent(ev inf.IEvent) error {
	ev.IncRef() // 增加引用
	select {
	case c.eventChannel <- ev: // 发送成功则里面释放

	default:
		ev.Release() // 发送不成功直接释放
		return def.ErrEventChannelIsFull
	}

	return nil
}

func (c *Cluster) run() {
	for {
		select {
		case ev := <-c.eventChannel:
			if ev != nil {
				switch ev.GetType() {
				case event.SysEventETCDPut, event.SysEventETCDDel:
					c.eventProcessor.EventHandler(ev)
					ev.Release()
				}
			}
		case <-c.closed:
			log.SysLogger.Info("cluster closed")
			return
		}
	}
}

func (c *Cluster) SetPid(pid *actor.PID) {
	// 这里没有实质内容,为了凑接口
}

func (c *Cluster) GetPid() *actor.PID {
	// 这里没有实质内容,为了凑接口
	return nil
}

func (c *Cluster) GetServerId() int32 {
	// 这里没有实质内容,为了凑接口
	return 0
}

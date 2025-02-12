// Package cluster
// @Title  集群模块
// @Description  desc
// @Author  pc  2024/11/5
// @Update  pc  2024/11/5
package cluster

import (
	"github.com/njtc406/emberengine/engine/cluster/discovery"
	"github.com/njtc406/emberengine/engine/cluster/endpoints"
	"github.com/njtc406/emberengine/engine/config"
	"github.com/njtc406/emberengine/engine/errdef"
	"github.com/njtc406/emberengine/engine/event"
	"github.com/njtc406/emberengine/engine/inf"
	"github.com/njtc406/emberengine/engine/utils/asynclib"
	"github.com/njtc406/emberengine/engine/utils/log"
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
	if err := c.discovery.Init(c.eventProcessor); err != nil {
		log.SysLogger.Fatalf("init discovery error:%v", err)
	}
}

func (c *Cluster) Start() {
	c.discovery.Start()
	c.endpoints.Start()
	asynclib.Go(func() {
		c.run()
	})
}

func (c *Cluster) Close() {
	close(c.closed)
	c.endpoints.Stop()
	c.discovery.Close()
}

func (c *Cluster) PushEvent(ev inf.IEvent) error {
	select {
	case c.eventChannel <- ev:
	default:
		return errdef.EventChannelIsFull
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
					e := ev.(*event.Event)
					c.eventProcessor.EventHandler(ev)
					event.ReleaseEvent(e)
				}
			}
		case <-c.closed:
			log.SysLogger.Info("cluster closed")
			return
		}
	}
}

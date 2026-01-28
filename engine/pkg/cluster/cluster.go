// Package cluster
// @Title  集群模块
// @Description  desc
// @Author  pc  2024/11/5
// @Update  pc  2024/11/5
package cluster

import (
	"context"

	"github.com/njtc406/emberengine/engine/pkg/cluster/discovery"
	_ "github.com/njtc406/emberengine/engine/pkg/cluster/discovery/etcd"
	"github.com/njtc406/emberengine/engine/pkg/cluster/endpoints"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
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
	eventProcessor *event.Processor
	eventChannel   chan inf.IEvent
}

// ctxEvent 包装 ctx 和 event
type ctxEvent struct {
	ctx context.Context
	ev  inf.IEvent
}

func (c *Cluster) Init() {
	c.closed = make(chan struct{})
	c.eventChannel = make(chan inf.IEvent, 1024)
	c.eventProcessor = event.NewTrigger()
	c.eventProcessor.Init(nil)

	c.endpoints = endpoints.GetEndpointManager().Init(c.eventProcessor)

	c.discovery = discovery.CreateDiscovery(config.Conf.ClusterConf.DiscoveryType)
	if c.discovery != nil {
		if err := c.discovery.Init(config.Conf.ClusterConf, c.eventProcessor, c); err != nil {
			log.SysLogger.Fatalf("init discovery error: %v, conf: %+v", err, config.Conf.ClusterConf)
		}
	}

	c.endpoints.SetClusterMode(c.IsClusterMode())
}

func (c *Cluster) Start() {
	if c.discovery != nil {
		c.discovery.Start()
	}

	c.endpoints.Start()
	go c.run()
}

func (c *Cluster) Close() {
	close(c.closed)
	c.endpoints.Stop()
	if c.discovery != nil {
		c.discovery.Close()
	}
}

func (c *Cluster) PushEvent(data inf.IEvent) error {
	if data == nil {
		return nil
	}
	select {
	case <-data.GetContext().Done():
		return data.GetContext().Err()
	case c.eventChannel <- data: // 发送成功则里面释放(需要阻塞等待,不能丢弃事件)
		//default:
		//	return def.ErrEventChannelIsFull
	}

	return nil
}

func (c *Cluster) run() {
	for {
		select {
		case evt, ok := <-c.eventChannel:
			if !ok {
				log.SysLogger.Error("cluster event channel closed")
				return
			}
			c.eventProcessor.Trigger(evt.GetContext(), evt.GetEventType(), evt.GetData())
		case <-c.closed:
			log.SysLogger.Info("cluster closed")
			return
		}
	}
}

func (c *Cluster) IsClusterMode() bool {
	return c.discovery != nil
}

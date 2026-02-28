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

// NewCluster 创建新的 Cluster 实例（Phase 2 per-Node 模式推荐使用）。
func NewCluster() *Cluster {
	return &Cluster{}
}

// SetCluster 设置全局 Cluster（向后兼容）。
// Deprecated: 请通过 NodeContext 获取。
func SetCluster(c *Cluster) {
	cluster = *c
}

// GetCluster 返回全局 Cluster 指针（向后兼容）。
// Deprecated: 请通过 NodeContext 获取。
func GetCluster() *Cluster {
	return &cluster
}

type Cluster struct {
	*log.Logger // 嵌入 Logger（替代 log.SysLogger）

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

func (c *Cluster) Init(clusterConf *config.ClusterConf, logger *log.Logger) {
	c.Logger = logger
	c.closed = make(chan struct{})
	c.eventChannel = make(chan inf.IEvent, 1024)
	c.eventProcessor = event.NewTrigger()
	c.eventProcessor.Init(nil)

	c.endpoints = endpoints.NewEndpointManager().Init(c.eventProcessor, clusterConf, logger)
	// 临时全局兼容
	endpoints.SetEndpointManager(c.endpoints)

	c.discovery = discovery.CreateDiscovery(clusterConf.DiscoveryType)
	if c.discovery != nil {
		if err := c.discovery.Init(clusterConf, c.eventProcessor, c); err != nil {
			c.Fatalf("init discovery error: %v, conf: %+v", err, clusterConf)
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
				c.Error("cluster event channel closed")
				return
			}
			c.eventProcessor.Trigger(evt.GetContext(), evt.GetEventType(), evt.GetData())
		case <-c.closed:
			c.Info("cluster closed")
			return
		}
	}
}

func (c *Cluster) IsClusterMode() bool {
	return c.discovery != nil
}

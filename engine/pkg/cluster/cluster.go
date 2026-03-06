// Package cluster
// @Title  集群模块
// @Description  desc
// @Author  pc  2024/11/5
// @Update  pc  2024/11/5
package cluster

import (
	"context"
	"fmt"

	"github.com/njtc406/emberengine/engine/pkg/cluster/discovery"
	_ "github.com/njtc406/emberengine/engine/pkg/cluster/discovery/etcd"
	"github.com/njtc406/emberengine/engine/pkg/cluster/endpoints"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/rpc/client"
	"github.com/njtc406/emberengine/engine/pkg/rpc/message/msgbus"
	remotehandler "github.com/njtc406/emberengine/engine/pkg/rpc/remote/handler"
)

// NewCluster 创建新的 Cluster 实例（Phase 2 per-Node 模式推荐使用）。
func NewCluster() *Cluster {
	return &Cluster{}
}

type Cluster struct {
	log.ILoggerX // 持有 ILoggerX，避免对 *log.Logger 的具体依赖

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

func (c *Cluster) Init(clusterConf *config.ClusterConf, logger log.ILoggerX, senderMgr *client.SenderManager, rpcHandler *remotehandler.Handler, natsConf *config.NatsConf, busFactory *msgbus.MessageBusFactory) error {
	c.ILoggerX = logger
	if c.ILoggerX == nil {
		return fmt.Errorf("cluster init requires logger")
	}
	c.closed = make(chan struct{})
	eventChannelSize := 1024
	if clusterConf != nil && clusterConf.EventChannelSize > 0 {
		eventChannelSize = clusterConf.EventChannelSize
	}
	c.eventChannel = make(chan inf.IEvent, eventChannelSize)
	c.eventProcessor = event.NewTrigger()
	c.eventProcessor.Init(nil)

	var err error
	c.endpoints, err = endpoints.NewEndpointManager().InitWithDeps(c.eventProcessor, clusterConf, c.ILoggerX, senderMgr, rpcHandler, natsConf, busFactory)
	if err != nil {
		return fmt.Errorf("init endpoints error: %w", err)
	}

	c.discovery = discovery.CreateDiscovery(clusterConf.DiscoveryType)
	if c.discovery != nil {
		if loggerAware, ok := c.discovery.(interface{ SetLogger(log.ILoggerX) }); ok {
			loggerAware.SetLogger(c.ILoggerX)
		}
		if err = c.discovery.Init(clusterConf, c.eventProcessor, c); err != nil {
			return fmt.Errorf("init discovery error: %w, conf: %+v", err, clusterConf)
		}
	}

	c.endpoints.SetClusterMode(c.IsClusterMode())
	return nil
}

func (c *Cluster) Start() error {
	if c.discovery != nil {
		c.discovery.Start()
	}

	if err := c.endpoints.Start(); err != nil {
		return err
	}
	go c.run()
	return nil
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

func (c *Cluster) GetEndpointManager() *endpoints.EndpointManager {
	return c.endpoints
}

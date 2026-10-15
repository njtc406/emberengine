// Package cluster
// @Title  集群模块
// @Description  desc
// @Author  pc  2024/11/5
// @Update  pc  2024/11/5
package cluster

import (
	"fmt"
	"hash/fnv"
	"sync"

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

	// sharded worker pool
	workerCount int
	shards      []chan inf.IEvent
	workerWg    sync.WaitGroup
	shardMu     sync.RWMutex
	closeOnce   sync.Once
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
	c.eventProcessor = event.NewTrigger()
	c.eventProcessor.Init(nil)

	// worker pool 配置
	c.workerCount = 1
	if clusterConf != nil && clusterConf.EventWorkerCount > 1 {
		c.workerCount = clusterConf.EventWorkerCount
	}
	c.shards = make([]chan inf.IEvent, c.workerCount)
	for i := range c.shards {
		c.shards[i] = make(chan inf.IEvent, eventChannelSize)
	}

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
	if err := c.endpoints.Start(); err != nil {
		return err
	}

	// 启动 shard workers
	for i := 0; i < c.workerCount; i++ {
		c.workerWg.Add(1)
		go c.shardWorker(i)
	}

	if c.discovery != nil {
		c.discovery.Start()
	}
	return nil
}

func (c *Cluster) Close() {
	c.closeOnce.Do(func() {
		close(c.closed)
		if c.discovery != nil {
			c.discovery.Close()
		}
		if c.endpoints != nil {
			c.endpoints.Stop()
		}

		c.shardMu.Lock()
		for i := range c.shards {
			close(c.shards[i])
		}
		c.shardMu.Unlock()

		c.workerWg.Wait()
	})
}

func (c *Cluster) PushEvent(data inf.IEvent) error {
	if data == nil {
		return nil
	}
	select {
	case <-data.GetContext().Done():
		return data.GetContext().Err()
	case <-c.closed:
		return fmt.Errorf("cluster closed")
	default:
	}

	shard := c.shardIndex(data)
	c.shardMu.RLock()
	defer c.shardMu.RUnlock()
	select {
	case <-c.closed:
		return fmt.Errorf("cluster closed")
	default:
	}

	select {
	case <-data.GetContext().Done():
		return data.GetContext().Err()
	case c.shards[shard] <- data:
	case <-c.closed:
		return fmt.Errorf("cluster closed")
	}
	return nil
}

// shardWorker 处理单个 shard 的事件队列。
func (c *Cluster) shardWorker(idx int) {
	defer c.workerWg.Done()
	for evt := range c.shards[idx] {
		c.eventProcessor.Trigger(evt.GetContext(), evt.GetEventType(), evt.GetData())
	}
}

// shardIndex 根据事件数据计算分片 index。
// 同一 key 的事件始终路由到同一 shard，保证顺序语义。
func (c *Cluster) shardIndex(evt inf.IEvent) int {
	if c.workerCount <= 1 {
		return 0
	}
	key := eventShardKey(evt)
	if key == "" {
		return 0 // 无法提取 key 的事件固定到 shard 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return int(h.Sum32()) % c.workerCount
}

// eventShardKey 从事件数据中提取用于分片的 key。
func eventShardKey(evt inf.IEvent) string {
	if evt == nil {
		return ""
	}
	// DiscoveryEvent 数据为 *mvccpb.KeyValue，用 etcd Key 作为分片依据
	type keyProvider interface {
		GetKey() []byte
	}
	if data := evt.GetData(); data != nil {
		if kp, ok := data.(keyProvider); ok {
			return string(kp.GetKey())
		}
	}
	return ""
}

func (c *Cluster) IsClusterMode() bool {
	return c.discovery != nil
}

func (c *Cluster) GetEndpointManager() *endpoints.EndpointManager {
	return c.endpoints
}

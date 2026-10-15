package node

import (
	"sort"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/event"
	"github.com/njtc406/emberengine/engine/pkg/metrics"
	"github.com/njtc406/emberengine/engine/pkg/rpc/client/pool"
	"github.com/njtc406/emberengine/engine/pkg/rpc/message/msgbus"
	"github.com/njtc406/emberengine/engine/pkg/services"
)

// RuntimeSnapshot 提供 Node 运行时关键状态快照。
type RuntimeSnapshot struct {
	NodeUID        string                       `json:"node_uid"`
	ClusterMode    bool                         `json:"cluster_mode"`
	UptimeSecs     int64                        `json:"uptime_secs"`
	Service        services.RuntimeSummary      `json:"service"`
	PoolMetrics    map[string]*pool.PoolMetrics `json:"pool_metrics"`
	PoolKeys       []string                     `json:"pool_keys"`
	RpcMetrics     *msgbus.RpcMetrics           `json:"rpc_metrics"`
	MailboxMetrics *def.MailboxMetrics          `json:"mailbox_metrics"`
	EventMetrics   *event.EventMetrics          `json:"event_metrics"`
}

// GetRuntimeSnapshot 返回当前 Node 的运行态快照。
func (n *Node) GetRuntimeSnapshot() RuntimeSnapshot {
	snapshot := RuntimeSnapshot{}
	if n == nil {
		return snapshot
	}

	snapshot.NodeUID = n.GetNodeUid()
	snapshot.ClusterMode = n.IsClusterMode()
	if !n.startTime.IsZero() {
		snapshot.UptimeSecs = int64(time.Since(n.startTime).Seconds())
	}

	if n.ServiceMgr != nil {
		snapshot.Service = n.ServiceMgr.GetRuntimeSummary()
		mbm := n.ServiceMgr.GetAggregatedMailboxMetrics()
		snapshot.MailboxMetrics = &mbm
	}

	if n.PoolManager != nil {
		snapshot.PoolMetrics = n.PoolManager.GetAllPoolMetrics()
		keys := make([]string, 0, len(snapshot.PoolMetrics))
		for k := range snapshot.PoolMetrics {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		snapshot.PoolKeys = keys
	}

	if n.BusFactory != nil {
		rm := n.BusFactory.GetRpcMetrics()
		snapshot.RpcMetrics = &rm
	}

	if n.EventBus != nil {
		snapshot.EventMetrics = n.EventBus.GetEventMetrics()
	}

	return snapshot
}

// ToSnapshotInfo 把 RuntimeSnapshot 转换为 metrics.SnapshotInfo，
// 供 /metrics 端点或外部采集使用。
func (s *RuntimeSnapshot) ToSnapshotInfo() *metrics.SnapshotInfo {
	return &metrics.SnapshotInfo{
		Node: metrics.NodeInfo{
			NodeUID:      s.NodeUID,
			UptimeSecs:   s.UptimeSecs,
			ServiceCount: s.Service.ServiceCount,
			ClusterMode:  s.ClusterMode,
		},
		PoolMetrics:    s.PoolMetrics,
		RpcMetrics:     s.RpcMetrics,
		MailboxMetrics: s.MailboxMetrics,
		EventMetrics:   s.EventMetrics,
	}
}

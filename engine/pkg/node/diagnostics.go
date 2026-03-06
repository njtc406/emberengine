package node

import (
	"sort"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/rpc/client/pool"
	"github.com/njtc406/emberengine/engine/pkg/services"
)

// RuntimeSnapshot 提供 Node 运行时关键状态快照。
type RuntimeSnapshot struct {
	NodeUID     string                       `json:"node_uid"`
	ClusterMode bool                         `json:"cluster_mode"`
	UptimeSecs  int64                        `json:"uptime_secs"`
	Service     services.RuntimeSummary      `json:"service"`
	PoolMetrics map[string]*pool.PoolMetrics `json:"pool_metrics"`
	PoolKeys    []string                     `json:"pool_keys"`
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

	return snapshot
}

package metrics

import (
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/event"
	pool "github.com/njtc406/emberengine/engine/pkg/rpc/client/pool"
	"github.com/njtc406/emberengine/engine/pkg/rpc/message/msgbus"
)

// SnapshotInfo 聚合 Node + Pool + RPC + Mailbox + Event 指标所需的信息。
// 由调用方（node 包或 HTTP handler）负责填充，metrics 包不反向依赖 node。
type SnapshotInfo struct {
	Node           NodeInfo
	PoolMetrics    map[string]*pool.PoolMetrics
	RpcMetrics     *msgbus.RpcMetrics
	MailboxMetrics *def.MailboxMetrics
	EventMetrics   *event.EventMetrics
}

// SnapshotToSamples 将 SnapshotInfo 转换为完整的指标样本列表。
// 输出顺序：Node → RPC → Mailbox → Event → Pool，保证 Prometheus text 输出稳定。
func SnapshotToSamples(info *SnapshotInfo) []MetricSample {
	if info == nil {
		return nil
	}

	nodeSamples := NodeSamples(info.Node)
	rpcSamples := RpcMetricsToSamples(info.RpcMetrics)
	mailboxSamples := MailboxMetricsToSamples(info.MailboxMetrics)
	eventSamples := EventMetricsToSamples(info.EventMetrics)
	poolSamples := PoolMetricsToSamples(info.PoolMetrics)

	result := make([]MetricSample, 0, len(nodeSamples)+len(rpcSamples)+len(mailboxSamples)+len(eventSamples)+len(poolSamples))
	result = append(result, nodeSamples...)
	result = append(result, rpcSamples...)
	result = append(result, mailboxSamples...)
	result = append(result, eventSamples...)
	result = append(result, poolSamples...)
	return result
}

// SnapshotToText 将 SnapshotInfo 转换为 Prometheus exposition text。
func SnapshotToText(info *SnapshotInfo) string {
	return SamplesToText(SnapshotToSamples(info))
}

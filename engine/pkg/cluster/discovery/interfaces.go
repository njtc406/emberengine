package discovery

import (
	"context"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// IDiscoveryServiceWatcher 负责前缀监听与初始状态同步
type IDiscoveryServiceWatcher interface {
	Start()
}

// IDiscoveryHealthMonitor 负责健康检查与重连
type IDiscoveryHealthMonitor interface {
	Start()
	Stop()
}

// LeaseRef 后端无关的租约引用句柄（不同实现可用不同具体类型）
type LeaseRef any

// ILeaseManager 租约管理器
type ILeaseManager interface {
	Grant(ttlSeconds int64) (LeaseRef, error)
	Revoke(ref LeaseRef)
	KeepAliveLoop(ctx context.Context, ref LeaseRef) error
}

// IServiceRegistry 服务注册接口
type IServiceRegistry interface {
	ServiceKey(pid *actor.PID) string
	MasterKey(group string) string
	RegisterService(ctx context.Context, pid *actor.PID, leaseRef LeaseRef) error
}

// IMasterElection 主选举接口
type IMasterElection interface {
	// TryAcquireMaster tries to acquire the master role for the given group.
	//
	// If succeeded is true, fencingToken is a monotonic token (backend-specific)
	// that can be used by upper layers to fence side effects.
	TryAcquireMaster(ctx context.Context, masterKey, group string, leaseRef LeaseRef) (succeeded bool, fencingToken int64, err error)
}

// IClientProvider 统一的客户端适配器
type IClientProvider interface {
	IsConnected() bool
	WatchPrefix(ctx context.Context, key string) <-chan clientv3.WatchResponse
	Watch(ctx context.Context, key string) <-chan clientv3.WatchResponse
	GetPrefix(ctx context.Context, key string) (*clientv3.GetResponse, error)
}

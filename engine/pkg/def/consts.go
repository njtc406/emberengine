// Package def
// @Title  常量定义
// @Description  desc
// @Author  yr  2024/11/6
// @Update  yr  2024/11/6
package def

import "time"

const (
	DefaultRpcConnNum           = 1
	DefaultRpcLenMsgLen         = 4
	DefaultRpcMinMsgLen         = 2
	DefaultMaxCheckCallRpcCount = 1000
	DefaultMaxPendingWriteNum   = 1000000

	DefaultConnectInterval             = 2 * time.Second
	DefaultCheckRpcCallTimeoutInterval = 1 * time.Second
	DefaultRpcTimeout                  = time.Second
)

const (
	ServiceStatusNormal int32 = iota
	ServiceStatusRetired
)

const (
	DefaultTimerSize         = 1024   // 默认定时器数量
	DefaultTimerBucketSize   = 1024   // 默认bucket数量
	DefaultUserMailboxSize   = 102400 // 默认事件队列数量
	DefaultSysMailboxSize    = 16     // 默认系统事件队列数量
	DefaultWorkerNum         = 1000   // 默认协程数量
	DefaultGoroutinePoolSize = 10     // 默认协程池大小
	DefaultVirtualWorkerRate = 10     // 虚拟worker比率
	DefaultEventChanSize     = 100    // 默认事件通道大小
)

const (
	SvcStatusUnknown  int32 = iota // 未运行
	SvcStatusInit                  // 初始化
	SvcStatusStarting              // 启动中
	SvcStatusRunning               // 运行中
	SvcStatusClosing               // 关闭中
	SvcStatusClosed                // 关闭
	SvcStatusRetire                // 退休
)

const (
	DefaultModuleIdSeed = 1_000_000 // 默认的moduleId开始序号
)

const (
	RpcTypeLocal = "local"
	RpcTypeRpcx  = "rpcx"
	RpcTypeGrpc  = "grpc"
	RpcTypeNats  = "nats"
)

const (
	DefaultPVPath            = "./cache"
	DefaultPVCPath           = "./data"
	DefaultLogPath           = "logs"
	DefaultAntsPoolSize      = 100
	DefaultProfilerInterval  = 10 * time.Second
	DefaultMonitorTimerSize  = 10000
	DefaultMonitorBucketSize = 20
)

const (
	DefaultServiceUse = "local"
)

const (
	DefaultDiscoveryUse = "etcd"
)

const (
	DiscoveryConfUseLocal  = "local"
	DiscoveryConfUseRemote = "remote"
)

const (
	NatsDefaultMaxReconnects      = 5 // 默认最大重连次数(0 表示未配置时会回落到该值)
	NatsDefaultReconnectWait      = 2 * time.Second
	NatsDefaultPingInterval       = 30 * time.Second
	NatsDefaultPingMaxOutstanding = 2
	NatsDefaultReconnectBufSize   = 1024 * 1024 * 8
	NatsDefaultTimeout            = 10 * time.Second
	// NatsDefaultSubPendingMsgLimit / NatsDefaultSubPendingBytesLimit
	// 用于提升异步订阅在高突发消息下的缓冲能力，避免默认 pending 限制触发 slow consumer 导致丢消息。
	// 注意：这不是“无上限”，仍然需要结合业务吞吐与内存预算评估。
	NatsDefaultSubPendingMsgLimit   = 200_000
	NatsDefaultSubPendingBytesLimit = 256 * 1024 * 1024
)

const (
	NatsDefaultGlobalPrefix = "event.global.%d"    // global.eventType
	NatsDefaultServerPrefix = "event.server.%d.%d" // server.eventType.partition
	NatsDefaultMasterPrefix = "event.master.%s"    // master.serviceUid
	NatsDefaultSlavePrefix  = "event.slave.%s"     // slave.serviceUid
	DefaultSpecificPrefix   = "event.specific.%d"  // specific.eventType
)

const NatsDefaultShardCount = 16

const NatsDefaultTopic = "ember.node."

const (
	DefaultTraceIdKey    = "ember.traceId"
	DefaultDispatcherKey = "ember.dispatchKey"
	DefaultTypeKey       = "ember.type"
	DefaultPriorityKey   = "ember.priority"

	// MasterEpochKey carries the current master fencing token (monotonic epoch) in event headers.
	// It is intended to help upper layers fence side effects under network partitions.
	MasterEpochKey = "ember.masterEpoch"
	// MasterPrevEpochKey carries the previous master fencing token in event headers.
	MasterPrevEpochKey = "ember.masterPrevEpoch"
)

const (
	ProtoBuf int32 = iota
	Json
)

const (
	DeDuplicatorTypeTTL = "ttl"
	DeDuplicatorTypeLRU = "lru"

	DefaultDeDuplicatorTTL = time.Second
)

const (
	WorkerTypeDefault = "default"
	WorkerTypeMulti   = "multi"
)

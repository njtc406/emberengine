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
	DefaultTimerSize         = 1024 // 默认定时器数量
	DefaultTimerBucketSize   = 1024 // 默认bucket数量
	DefaultUserMailboxSize   = 1024 // 默认事件队列数量
	DefaultSysMailboxSize    = 16   // 默认系统事件队列数量
	DefaultWorkerNum         = 1    // 默认协程数量
	DefaultGoroutinePoolSize = 10   // 默认协程池大小
	DefaultVirtualWorkerRate = 10   // 虚拟worker比率
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
	PriorityUser    int32 = 0 // 用户事件
	PriorityUserStr       = "0"
	PrioritySys     int32 = 1 // 系统事件
	PrioritySysStr        = "1"
)

const (
	NatsDefaultMaxReconnects      = 5 // 不限制重连次数
	NatsDefaultReconnectWait      = 2 * time.Second
	NatsDefaultPingInterval       = 30 * time.Second
	NatsDefaultPingMaxOutstanding = 2
	NatsDefaultReconnectBufSize   = 1024 * 1024 * 8
	NatsDefaultTimeout            = 10 * time.Second
)

const (
	NatsDefaultGlobalPrefix = "event.global.%d"    // global.eventType
	NatsDefaultServerPrefix = "event.server.%d.%d" // server.eventType.serverId
	NatsDefaultMasterPrefix = "event.master.%s"    // master.serviceUid
	NatsDefaultSlavePrefix  = "event.slave.%s"     // slave.serviceUid
)

const NatsDefaultShardCount = 16

const NatsDefaultTopic = "rpc.node.%s"

const DefaultReqDuplicatorTTL = 1 * time.Second

const (
	DefaultTraceIdKey    = "sys.traceId"
	DefaultDispatcherKey = "sys.dispatchKey"
	DefaultTypeKey       = "sys.type"
	DefaultPriorityKey   = "sys.priority"
)

const (
	ProtoBuf int32 = iota
	Json
)

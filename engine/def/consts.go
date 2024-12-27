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
	DefaultTimerSize          = 1024 // 默认定时器数量
	DefaultMailBoxSize        = 1024 // 默认事件队列数量
	DefaultGoroutineNum int32 = 1    // 默认协程数量
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
)

const (
	DefaultPVPath           = "./cache"
	DefaultPVCPath          = "./data"
	DefaultLogPath          = "logs"
	DefaultAntsPoolSize     = 10000
	DefaultProfilerInterval = 10 * time.Second
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

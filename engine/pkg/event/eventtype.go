// Package event
// @Title  事件类型
// @Description  事件类型
// @Author  yr  2024/7/19 下午3:40
// @Update  yr  2024/7/19 下午3:40
package event

const (
	// TODO 找个时间把这个换成proto枚举,打印的时候可以直接打名称

	// 基础事件 -1000以上 系统事件 -1 到 -999  用户事件 1 - 999
	SysEventWebsocket = -5

	SysEventETCDPut       = -11 // etcd 存储事件
	SysEventETCDDel       = -12 // etcd 删除事件
	SysEventServiceReg    = -13 // 服务注册事件
	SysEventServiceDis    = -14 // 服务注销事件
	SysEventServiceUpdate = -15 // 服务更新事件

	SysEventServiceUp         = -30 // 服务上线事件
	SysEventServiceDown       = -31 // 服务下线事件
	SysEventServiceConfChange = -32 // 服务配置变更事件
	SysEventServiceReload     = -33 // 服务重载事件

	SysEventNodeConn = -50 // 节点连接事件
	SysEventNatsConn = -51 // nats 连接事件

	ServiceSuspended     = -1001 // 服务挂起消息事件
	ServiceResumed       = -1002 // 服务恢复消息事件
	SysEventServiceClose = -1003 // 服务关闭事件
	ServiceNew           = -1004 // 启动服务
	ServiceClose         = -1005 // 关闭服务
	ServiceHeartbeat     = -1006 // 心跳事件

	SysEventKcp       = -1100 // kcp 连接事件
	SysEventTcp       = -1101 // tcp 连接事件
	SysEventWebSocket = -1102

	ServiceConcurrentCallback = -2001 // 并发回调事件
	ServiceTimerCallback      = -2002 // 定时器回调事件
	ServiceGlobalEventTrigger = -2003 // 全局事件系统事件回调

	UnknownEvent = -3000 // 未知事件
	RpcMsg       = -3001 // rpc 消息事件

	ServiceBecomeMaster = -4004 // 升级为主服务
	ServiceLoseMaster   = -4005 // 降级为从服务
	ServiceBecomeSlaver = -4006 // 抢主结束时为从服务
	ServiceDisconnected = -4007 // 服务从集群断开(当该服务的服务发现watcher重启超过最大次数时,会有这个事件,服务收到后应该尝试重启或者下线之类的操作)

	MaxType = -1 // 预定义的最大只到-1
)

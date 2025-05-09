// Package event
// @Title  事件类型
// @Description  事件类型
// @Author  yr  2024/7/19 下午3:40
// @Update  yr  2024/7/19 下午3:40
package event

const (

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
	ServiceMasterEventTrigger = -2004 // master 事件系统事件回调
	ServiceSlaverEventTrigger = -2005 // slave 事件系统事件回调

	RpcMsg = -3001 // rpc 消息事件

	MaxType = -1 // 预定义的最大只到-1
)

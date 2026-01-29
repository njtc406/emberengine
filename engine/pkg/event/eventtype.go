// Package event
// @Title  事件类型
// @Description  事件类型
// @Author  yr  2024/7/19 下午3:40
// @Update  yr  2024/7/19 下午3:40
package event

import inf "github.com/njtc406/emberengine/engine/pkg/interfaces"

const (
	// TODO 找个时间把这个换成proto枚举,打印的时候可以直接打名称

	// 基础事件 -1000以上 系统事件 -1 到 -999  用户事件 1 - 999
	SysEventWebsocket inf.EventType = -5

	SysEventETCDPut       inf.EventType = -11 // etcd 存储事件
	SysEventETCDDel       inf.EventType = -12 // etcd 删除事件
	SysEventServiceReg    inf.EventType = -13 // 服务注册事件
	SysEventServiceDis    inf.EventType = -14 // 服务注销事件
	SysEventServiceUpdate inf.EventType = -15 // 服务更新事件

	SysEventServiceUp         inf.EventType = -30 // 服务上线事件
	SysEventServiceDown       inf.EventType = -31 // 服务下线事件
	SysEventServiceConfChange inf.EventType = -32 // 服务配置变更事件
	SysEventServiceReload     inf.EventType = -33 // 服务重载事件

	SysEventNodeConn inf.EventType = -50 // 节点连接事件
	SysEventNatsConn inf.EventType = -51 // nats 连接事件

	ServiceSuspended     inf.EventType = -1001 // 服务挂起消息事件
	ServiceResumed       inf.EventType = -1002 // 服务恢复消息事件
	SysEventServiceClose inf.EventType = -1003 // 服务关闭事件
	ServiceNew           inf.EventType = -1004 // 启动服务
	ServiceClose         inf.EventType = -1005 // 关闭服务
	ServiceHeartbeat     inf.EventType = -1006 // 心跳事件
	ServiceFinalize      inf.EventType = -1007 // 服务最终清理事件（在 mailbox 内执行）

	SysEventKcp       inf.EventType = -1100 // kcp 连接事件
	SysEventTcp       inf.EventType = -1101 // tcp 连接事件
	SysEventWebSocket inf.EventType = -1102

	ServiceConcurrentCallback inf.EventType = -2001 // 并发回调事件
	ServiceTimerCallback      inf.EventType = -2002 // 定时器回调事件
	ServiceGlobalEventTrigger inf.EventType = -2003 // 全局事件系统事件回调

	UnknownEvent inf.EventType = -3000 // 未知事件
	RpcMsg       inf.EventType = -3001 // rpc 消息事件

	ServiceBecomeMaster inf.EventType = -4004 // 升级为主服务
	ServiceLoseMaster   inf.EventType = -4005 // 降级为从服务
	ServiceBecomeSlaver inf.EventType = -4006 // 抢主结束时为从服务
	ServiceDisconnected inf.EventType = -4007 // 服务从集群断开(当该服务的服务发现watcher重启超过最大次数时,会有这个事件,服务收到后应该尝试重启或者下线之类的操作)

	MaxType inf.EventType = -1 // 预定义的最大只到-1
)

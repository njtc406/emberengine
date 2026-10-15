// Package event
// @Title  事件类型
// @Description  事件类型
// @Author  yr  2024/7/19 下午3:40
// @Update  yr  2024/7/19 下午3:40
package event

import (
	"github.com/njtc406/emberengine/engine/pkg/def"
)

const (
	// TODO 找个时间把这个换成proto枚举,打印的时候可以直接打名称

	// 基础事件 -1000以上 系统事件 -1 到 -999  用户事件 1 - 999
	SysEventWebsocket def.EventType = -5

	SysEventETCDPut       def.EventType = -11 // etcd 存储事件
	SysEventETCDDel       def.EventType = -12 // etcd 删除事件
	SysEventServiceReg    def.EventType = -13 // 服务注册事件
	SysEventServiceDis    def.EventType = -14 // 服务注销事件
	SysEventServiceUpdate def.EventType = -15 // 服务更新事件

	SysEventServiceUp         def.EventType = -30 // 服务上线事件
	SysEventServiceDown       def.EventType = -31 // 服务下线事件
	SysEventServiceConfChange def.EventType = -32 // 服务配置变更事件
	SysEventServiceReload     def.EventType = -33 // 服务重载事件

	SysEventNodeConn def.EventType = -50 // 节点连接事件
	SysEventNatsConn def.EventType = -51 // nats 连接事件

	ServiceSuspended     def.EventType = -1001 // 服务挂起消息事件
	ServiceResumed       def.EventType = -1002 // 服务恢复消息事件
	SysEventServiceClose def.EventType = -1003 // 服务关闭事件
	ServiceNew           def.EventType = -1004 // 启动服务
	ServiceClose         def.EventType = -1005 // 关闭服务
	ServiceHeartbeat     def.EventType = -1006 // 心跳事件
	ServiceFinalize      def.EventType = -1007 // 服务最终清理事件（在 mailbox 内执行）

	SysEventKcp       def.EventType = -1100 // kcp 连接事件
	SysEventTcp       def.EventType = -1101 // tcp 连接事件
	SysEventWebSocket def.EventType = -1102

	ServiceConcurrentCallback def.EventType = -2001 // 并发回调事件
	ServiceTimerCallback      def.EventType = -2002 // 定时器回调事件
	ServiceGlobalEventTrigger def.EventType = -2003 // 全局事件系统事件回调

	UnknownEvent def.EventType = -3000 // 未知事件
	RpcMsg       def.EventType = -3001 // rpc 消息事件

	ServiceBecomeMaster def.EventType = -4004 // 升级为主服务
	ServiceLoseMaster   def.EventType = -4005 // 降级为从服务
	ServiceBecomeSlaver def.EventType = -4006 // 抢主结束时为从服务
	ServiceDisconnected def.EventType = -4007 // 服务从集群断开(当该服务的服务发现watcher重启超过最大次数时,会有这个事件,服务收到后应该尝试重启或者下线之类的操作)

	MaxType def.EventType = -1 // 预定义的最大只到-1
)

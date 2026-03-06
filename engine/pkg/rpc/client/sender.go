// Package client
// @Title  消息发送器
// @Description  用来向对应的服务发送消息
// @Author  yr  2024/11/7
// @Update  yr  2024/11/7
package client

import (
	"context"
	"sync"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/monitor"
	"github.com/njtc406/emberengine/engine/pkg/rpc/client/pool"
)

// ── 默认创建器注册表（返回副本，避免全局可变 map） ──

func defaultSenderMap(logger log.ILoggerX, natsConf *config.NatsConf, grpcConnNum int) map[string]SenderCreator {
	return map[string]SenderCreator{
		def.RpcTypeRpcx: func(addr string) inf.IRpcSender {
			return newRpcxClient(addr, logger)
		},
		def.RpcTypeGrpc: func(addr string) inf.IRpcSender {
			return newGrpcClient(addr, logger, grpcConnNum)
		},
		def.RpcTypeNats: func(addr string) inf.IRpcSender {
			return newNatsClient(addr, logger, natsConf)
		},
	}
}

type SenderCreator func(addr string) inf.IRpcSender

// ── SenderManager 结构体 ──

// SenderManager 管理所有 RPC sender 的创建和缓存。
// 取代原来的包级 senderMap / senderHandlerMap / lock / init() / Close()。
type SenderManager struct {
	log.ILoggerX // 持有 ILoggerX

	poolMgr     *pool.PoolManager
	rpcMonitor  *monitor.RpcMonitor
	natsConf    *config.NatsConf
	grpcConnNum int
	senderMap   map[string]SenderCreator             // 协议 → 创建器（初始化后只读）
	handlerMap  map[string]map[string]inf.IRpcSender // map[addr][tp]sender
	handlerLock sync.RWMutex
}

// NewSenderManager 创建 SenderManager 并向 PoolManager 注册远程创建器。
func NewSenderManager(poolMgr *pool.PoolManager, logger log.ILoggerX, rpcMonitor *monitor.RpcMonitor, natsConf *config.NatsConf, grpcConnNum int) *SenderManager {
	mgr := &SenderManager{
		ILoggerX:    logger,
		poolMgr:     poolMgr,
		rpcMonitor:  rpcMonitor,
		natsConf:    natsConf,
		grpcConnNum: grpcConnNum,
		senderMap:   defaultSenderMap(logger, natsConf, grpcConnNum),
		handlerMap:  make(map[string]map[string]inf.IRpcSender),
	}
	mgr.registerCreators()
	return mgr
}

// registerCreators 将非本地创建器注册到 PoolManager。
func (sm *SenderManager) registerCreators() {
	for rpcType, creator := range sm.senderMap {
		if rpcType != def.RpcTypeLocal {
			sm.poolMgr.RegisterCreator(rpcType, pool.SenderCreator(creator))
		}
	}
}

// Register 注册自定义消息发送器
func (sm *SenderManager) Register(tp string, creator SenderCreator) {
	sm.senderMap[tp] = creator
}

func (sm *SenderManager) getSenderHandler(addr string, tp string) inf.IRpcSender {
	sm.handlerLock.RLock()
	if tps, ok := sm.handlerMap[addr]; ok {
		if handler, ok := tps[tp]; ok {
			sm.handlerLock.RUnlock()
			return handler
		}
		sm.handlerLock.RUnlock()
		return sm.addSenderHandler(addr, tp)
	}
	sm.handlerLock.RUnlock()
	return sm.addSenderHandler(addr, tp)
}

func (sm *SenderManager) addSenderHandler(addr, tp string) inf.IRpcSender {
	sm.handlerLock.Lock()
	defer sm.handlerLock.Unlock()

	if tps, ok := sm.handlerMap[addr]; ok {
		if handler, ok := tps[tp]; ok {
			return handler
		}
		handler := sm.senderMap[tp](addr)
		tps[tp] = handler
		return handler
	}

	handler := sm.senderMap[tp](addr)
	sm.handlerMap[addr] = map[string]inf.IRpcSender{tp: handler}
	return handler
}

// Close 关闭所有 sender
func (sm *SenderManager) Close() {
	sm.handlerLock.Lock()
	defer sm.handlerLock.Unlock()
	for _, tps := range sm.handlerMap {
		for _, handler := range tps {
			handler.Close()
		}
	}
	sm.handlerMap = make(map[string]map[string]inf.IRpcSender)
}

// ── Dispatcher ──

type Dispatcher struct {
	tmp bool // 是否是临时客户端
	pid *actor.PID
	sm  *SenderManager

	inf.IMailboxChannel
	localHandler inf.IRpcSender
}

func (c *Dispatcher) GetPid() *actor.PID {
	return c.pid
}

func (c *Dispatcher) SetPid(pid *actor.PID) {
	c.pid = pid
}

func (c *Dispatcher) Close() {
	c.pid = nil
}

func (c *Dispatcher) IsClosed() bool {
	return c.pid == nil
}

func (c *Dispatcher) getSender() inf.IRpcSender {
	sm := c.sm
	if sm == nil {
		return nil
	}
	if c.IMailboxChannel != nil {
		// 本地节点的sender
		if c.localHandler == nil {
			c.localHandler = newLClient("", sm.rpcMonitor)
		}
		return c.localHandler
	}
	return sm.getSenderHandler(c.pid.GetAddress(), c.pid.GetRpcType())
}

func (c *Dispatcher) DeliverRequest(ctx context.Context, envelope inf.IEnvelope) error {
	if c.pid == nil {
		return def.ErrServiceNotFound
	}
	sender := c.getSender()
	if sender == nil {
		return def.ErrRPCHadClosed
	}
	return sender.DeliverRequest(ctx, c, envelope)
}

func (c *Dispatcher) DeliverResponse(ctx context.Context, envelope inf.IEnvelope) error {
	if c.pid == nil {
		return def.ErrServiceNotFound
	}
	sender := c.getSender()
	if sender == nil {
		return def.ErrRPCHadClosed
	}
	return sender.DeliverResponse(ctx, c, envelope)
}

func NewDispatcher(sm *SenderManager, pid *actor.PID, mailbox inf.IMailboxChannel) inf.IRpcDispatcher {
	return &Dispatcher{
		pid:             pid,
		sm:              sm,
		IMailboxChannel: mailbox,
	}
}

func NewTmpDispatcher(sm *SenderManager, pid *actor.PID, mailbox inf.IMailboxChannel) inf.IRpcDispatcher {
	return &Dispatcher{
		tmp:             true,
		pid:             pid,
		sm:              sm,
		IMailboxChannel: mailbox,
	}
}

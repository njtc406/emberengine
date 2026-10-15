// Package client
// @Title  增强发送器
// @Description  集成连接池管理和熔断器的高级发送器
// @Author  yr  2025/1/20
// @Update  yr  2025/1/20
package client

import (
	"context"
	"time"

	"github.com/njtc406/emberengine/v2/engine/pkg/log"

	"github.com/njtc406/emberengine/v2/engine/pkg/def"
	inf "github.com/njtc406/emberengine/v2/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/v2/engine/pkg/rpc/client/pool"
)

// EnhancedSender 增强的发送器，支持连接池和熔断器
type EnhancedSender struct {
	address string
	rpcType string
	poolMgr *pool.PoolManager
	logger  log.ILoggerX
}

// NewEnhancedSender 使用节点持有的连接池和日志器创建增强发送器。
// 连接池的注册、配置和关闭仍由调用方负责。
func NewEnhancedSender(address, rpcType string, poolMgr *pool.PoolManager, logger log.ILoggerX) *EnhancedSender {
	return &EnhancedSender{
		address: address,
		rpcType: rpcType,
		poolMgr: poolMgr,
		logger:  logger,
	}
}

// SendRequest 发送请求并转移 envelope 所有权，调用后不得再使用 envelope。
func (es *EnhancedSender) SendRequest(dispatcher inf.IRpcDispatcher, envelope inf.IEnvelope) error {
	return es.deliver(dispatcher, envelope, false)
}

// SendRequestAndRelease 与 SendRequest 相同，底层 DeliverRequest 已负责释放 envelope。
func (es *EnhancedSender) SendRequestAndRelease(dispatcher inf.IRpcDispatcher, envelope inf.IEnvelope) error {
	return es.SendRequest(dispatcher, envelope)
}

// SendResponse 发送响应并转移 envelope 所有权。
func (es *EnhancedSender) SendResponse(dispatcher inf.IRpcDispatcher, envelope inf.IEnvelope) error {
	return es.deliver(dispatcher, envelope, true)
}

func (es *EnhancedSender) deliver(dispatcher inf.IRpcDispatcher, envelope inf.IEnvelope, response bool) error {
	start := time.Now()
	conn, err := es.poolMgr.GetConnection(es.address, es.rpcType)
	if err != nil {
		if es.logger != nil {
			es.logger.Errorf("获取连接失败: %v", err)
		}
		envelope.Release()
		return err
	}
	if !conn.IsHealthy() {
		if es.logger != nil {
			es.logger.Warnf("连接 %s 不健康，跳过发送", conn.ID)
		}
		envelope.Release()
		return def.ErrRPCHadClosed
	}
	conn.SetState(pool.StateActive)
	if response {
		err = conn.Sender.DeliverResponse(context.Background(), dispatcher, envelope)
	} else {
		err = conn.Sender.DeliverRequest(context.Background(), dispatcher, envelope)
	}
	conn.UpdateMetrics(err == nil, time.Since(start))
	conn.SetState(pool.StateIdle)
	if err != nil && es.logger != nil {
		es.logger.Errorf("发送失败: %v", err)
	}
	return err
}

// Close 关闭发送器
func (es *EnhancedSender) Close() {
	// 增强发送器本身不直接管理连接，由连接池管理器负责
	// 这里可以移除连接池（如果需要的话）
}

// IsClosed 检查是否已关闭
func (es *EnhancedSender) IsClosed() bool {
	// 检查连接池中是否有健康的连接
	metrics, err := es.poolMgr.GetPoolMetrics(es.address, es.rpcType)
	if err != nil {
		return true
	}

	return metrics.TotalConnections == 0
}

// GetMetrics 获取发送器指标
func (es *EnhancedSender) GetMetrics() (*pool.PoolMetrics, error) {
	return es.poolMgr.GetPoolMetrics(es.address, es.rpcType)
}

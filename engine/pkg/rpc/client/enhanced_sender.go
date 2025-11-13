// Package client
// @Title  增强发送器
// @Description  集成连接池管理和熔断器的高级发送器
// @Author  yr  2025/1/20
// @Update  yr  2025/1/20
package client

import (
	"time"

	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/rpc/client/pool"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
)

// EnhancedSender 增强的发送器，支持连接池和熔断器
type EnhancedSender struct {
	address string
	rpcType string
	poolMgr *pool.PoolManager
}

// NewEnhancedSender 创建新的增强发送器
func NewEnhancedSender(address, rpcType string) *EnhancedSender {
	return &EnhancedSender{
		address: address,
		rpcType: rpcType,
		poolMgr: pool.GetGlobalPoolManager(),
	}
}

// SendRequest 发送请求
func (es *EnhancedSender) SendRequest(dispatcher inf.IRpcDispatcher, envelope inf.IEnvelope) error {
	start := time.Now()

	conn, err := es.poolMgr.GetConnection(es.address, es.rpcType)
	if err != nil {
		log.SysLogger.Errorf("获取连接失败: %v", err)
		return err
	}

	// 检查熔断器状态
	if !conn.IsHealthy() {
		log.SysLogger.Warnf("连接 %s 不健康，跳过请求", conn.ID)
		return def.ErrRPCHadClosed
	}

	// 更新连接状态为活跃
	conn.SetState(pool.StateActive)

	// 执行实际请求
	err = conn.Sender.SendRequest(dispatcher, envelope)

	// 记录请求结果和更新指标
	responseTime := time.Since(start)
	success := err == nil
	conn.UpdateMetrics(success, responseTime)

	// 恢复连接状态为空闲
	conn.SetState(pool.StateIdle)

	if !success {
		log.SysLogger.Errorf("发送请求失败: %v", err)
	}

	return err
}

// SendRequestAndRelease 发送请求并释放envelope
func (es *EnhancedSender) SendRequestAndRelease(dispatcher inf.IRpcDispatcher, envelope inf.IEnvelope) error {
	defer envelope.Release()
	return es.SendRequest(dispatcher, envelope)
}

// SendResponse 发送响应
func (es *EnhancedSender) SendResponse(dispatcher inf.IRpcDispatcher, envelope inf.IEnvelope) error {
	start := time.Now()

	conn, err := es.poolMgr.GetConnection(es.address, es.rpcType)
	if err != nil {
		log.SysLogger.Errorf("获取连接失败: %v", err)
		return err
	}

	// 检查熔断器状态
	if !conn.IsHealthy() {
		log.SysLogger.Warnf("连接 %s 不健康，跳过响应", conn.ID)
		return def.ErrRPCHadClosed
	}

	// 更新连接状态为活跃
	conn.SetState(pool.StateActive)

	// 执行实际响应
	err = conn.Sender.SendResponse(dispatcher, envelope)

	// 记录请求结果和更新指标
	responseTime := time.Since(start)
	success := err == nil
	conn.UpdateMetrics(success, responseTime)

	// 恢复连接状态为空闲
	conn.SetState(pool.StateIdle)

	if !success {
		log.SysLogger.Errorf("发送响应失败: %v", err)
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

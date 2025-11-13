// Package pool
// @Title  连接池工厂
// @Description  连接池工厂，管理所有地址和类型的连接池
// @Author  yr  2025/1/20
// @Update  yr  2025/1/20
package pool

import (
	"fmt"
	"sync"

	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
)

// SenderCreator 发送器创建函数类型
type SenderCreator func(addr string) inf.IRpcSender

// PoolManager 连接池管理器
type PoolManager struct {
	pools       map[string]*ConnectionPool // key: addr_type
	poolMutex   sync.RWMutex
	creators    map[string]SenderCreator // RPC类型对应的创建器
	poolConfigs map[string]*PoolConfig   // 每种RPC类型的配置
}

// NewPoolManager 创建新的连接池管理器
func NewPoolManager() *PoolManager {
	return &PoolManager{
		pools:       make(map[string]*ConnectionPool),
		creators:    make(map[string]SenderCreator),
		poolConfigs: make(map[string]*PoolConfig),
	}
}

// RegisterCreator 注册RPC发送器创建器
func (pm *PoolManager) RegisterCreator(rpcType string, creator SenderCreator) {
	pm.poolMutex.Lock()
	defer pm.poolMutex.Unlock()
	pm.creators[rpcType] = creator
}

// SetPoolConfig 设置特定RPC类型的连接池配置
func (pm *PoolManager) SetPoolConfig(rpcType string, config *PoolConfig) {
	pm.poolMutex.Lock()
	defer pm.poolMutex.Unlock()
	pm.poolConfigs[rpcType] = config
}

// GetOrCreatePool 获取或创建连接池
func (pm *PoolManager) GetOrCreatePool(address, rpcType string) (*ConnectionPool, error) {
	poolKey := fmt.Sprintf("%s_%s", address, rpcType)

	// 首先尝试读锁获取
	pm.poolMutex.RLock()
	if pool, exists := pm.pools[poolKey]; exists {
		pm.poolMutex.RUnlock()
		return pool, nil
	}
	pm.poolMutex.RUnlock()

	// 需要创建新的连接池，使用写锁
	pm.poolMutex.Lock()
	defer pm.poolMutex.Unlock()

	// 双重检查，防止并发创建
	if pool, exists := pm.pools[poolKey]; exists {
		return pool, nil
	}

	// 检查是否有对应的创建器
	creator, exists := pm.creators[rpcType]
	if !exists {
		return nil, fmt.Errorf("no creator registered for RPC type: %s", rpcType)
	}

	// 获取配置，如果没有则使用默认配置
	config := pm.poolConfigs[rpcType]
	if config == nil {
		config = DefaultPoolConfig()
		// 根据RPC类型调整默认配置
		switch rpcType {
		case "rpcx":
			config.InitialConnections = 4
			config.MaxConnections = 20
		case "grpc":
			config.InitialConnections = 2
			config.MaxConnections = 10
		case "nats":
			config.InitialConnections = 1
			config.MaxConnections = 5
		}
	}

	// 创建新的连接池
	pool := NewConnectionPool(address, rpcType, creator, config)

	// 启动连接池
	if err := pool.Start(); err != nil {
		return nil, fmt.Errorf("failed to start connection pool for %s:%s: %w", address, rpcType, err)
	}

	pm.pools[poolKey] = pool
	log.SysLogger.Infof("创建新的连接池: %s, 类型: %s", address, rpcType)

	return pool, nil
}

// GetConnection 获取连接
func (pm *PoolManager) GetConnection(address, rpcType string) (*PoolConnection, error) {
	pool, err := pm.GetOrCreatePool(address, rpcType)
	if err != nil {
		return nil, err
	}

	return pool.GetConnection()
}

// Close 关闭所有连接池
func (pm *PoolManager) Close() {
	pm.poolMutex.Lock()
	defer pm.poolMutex.Unlock()

	for poolKey, pool := range pm.pools {
		pool.Stop()
		log.SysLogger.Infof("关闭连接池: %s", poolKey)
	}

	// 清空池映射
	pm.pools = make(map[string]*ConnectionPool)
}

// GetPoolMetrics 获取指定连接池的指标
func (pm *PoolManager) GetPoolMetrics(address, rpcType string) (*PoolMetrics, error) {
	poolKey := fmt.Sprintf("%s_%s", address, rpcType)

	pm.poolMutex.RLock()
	defer pm.poolMutex.RUnlock()

	pool, exists := pm.pools[poolKey]
	if !exists {
		return nil, fmt.Errorf("pool not found for %s:%s", address, rpcType)
	}

	return pool.GetMetrics(), nil
}

// GetAllPoolMetrics 获取所有连接池的指标
func (pm *PoolManager) GetAllPoolMetrics() map[string]*PoolMetrics {
	pm.poolMutex.RLock()
	defer pm.poolMutex.RUnlock()

	result := make(map[string]*PoolMetrics)
	for poolKey, pool := range pm.pools {
		result[poolKey] = pool.GetMetrics()
	}

	return result
}

// RemovePool 移除指定的连接池
func (pm *PoolManager) RemovePool(address, rpcType string) {
	poolKey := fmt.Sprintf("%s_%s", address, rpcType)

	pm.poolMutex.Lock()
	defer pm.poolMutex.Unlock()

	if pool, exists := pm.pools[poolKey]; exists {
		pool.Stop()
		delete(pm.pools, poolKey)
		log.SysLogger.Infof("移除连接池: %s", poolKey)
	}
}

// 全局连接池管理器实例
var globalPoolManager *PoolManager
var poolManagerOnce sync.Once

// GetGlobalPoolManager 获取全局连接池管理器
func GetGlobalPoolManager() *PoolManager {
	poolManagerOnce.Do(func() {
		globalPoolManager = NewPoolManager()
	})
	return globalPoolManager
}

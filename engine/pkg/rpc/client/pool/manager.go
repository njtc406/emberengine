// Package pool
// @Title  连接池管理器
// @Description  动态扩缩容连接池管理,支持健康监控和智能路由
// @Author  yr  2025/1/20
// @Update  yr  2025/1/20
package pool

import (
	"context"
	"sync"
	"time"

	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
)

// ConnectionPool 增强连接池
type ConnectionPool struct {
	config  *PoolConfig
	address string
	rpcType string
	creator func(addr string) inf.IRpcSender
	logger  log.ILoggerX

	connections map[string]*PoolConnection
	connMutex   sync.RWMutex

	metrics *PoolMetrics

	// 负载均衡
	roundRobin int64

	// 扩缩容控制
	lastScaleUp   time.Time
	lastScaleDown time.Time
	scaleMutex    sync.Mutex

	// 健康检查
	healthTicker *time.Ticker
	stopHealth   chan struct{}

	// 生命周期管理
	cleanupTicker *time.Ticker
	stopCleanup   chan struct{}

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewConnectionPool 创建新连接池
func NewConnectionPool(address, rpcType string, creator func(addr string) inf.IRpcSender, config *PoolConfig, logger log.ILoggerX) *ConnectionPool {
	if config == nil {
		config = DefaultPoolConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	pool := &ConnectionPool{
		config:      config,
		address:     address,
		rpcType:     rpcType,
		creator:     creator,
		logger:      logger,
		connections: make(map[string]*PoolConnection),
		metrics:     &PoolMetrics{},
		stopHealth:  make(chan struct{}),
		stopCleanup: make(chan struct{}),
		ctx:         ctx,
		cancel:      cancel,
	}

	return pool
}

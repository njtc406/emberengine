package mailbox

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/dto"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
)

const dispatchKeyShards = 16 // 分段数，必须为 2 的幂

// dispatchKeyShard 单个分段，独立锁保护自己的 map
type dispatchKeyShard struct {
	mu     sync.Mutex
	counts map[string]uint64
	total  uint64
}

// DispatchKeyStatsMiddleware is a debug helper that reports dispatcherKey hot spots.
// It counts keys over an interval and logs top-N periodically.
//
// 使用分段锁（sharded map）降低热路径上的锁竞争。
// Designed to be enabled only in debug mode.
// Implements IMailboxMiddleware interface.
type DispatchKeyStatsMiddleware struct {
	logger   log.ILoggerX
	interval time.Duration
	topN     int
	maxKeys  int

	stopOnce  sync.Once
	stopCh    chan struct{}
	doneCh    chan struct{} // 后台 goroutine 实际退出信号，供 OnStop 等待
	running   atomic.Bool
	totalKeys atomic.Int64 // 全局 key 总数（跨 shard 累加），用于统一裁剪

	shards [dispatchKeyShards]dispatchKeyShard
}

func NewDispatchKeyStatsMiddleware(logger log.ILoggerX, interval time.Duration, topN int, maxKeys int) *DispatchKeyStatsMiddleware {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	if topN <= 0 {
		topN = 10
	}
	if maxKeys <= 0 {
		maxKeys = 100_000
	}
	m := &DispatchKeyStatsMiddleware{
		logger:   logger,
		interval: interval,
		topN:     topN,
		maxKeys:  maxKeys,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
	for i := range m.shards {
		m.shards[i].counts = make(map[string]uint64, 64)
	}
	return m
}

// shardFor 根据 key 的 fnv-like hash 定位到分段。
// 为避免长 key 的 O(len) 哈希成本，最多采样前 32 字节（对调试统计而言
// 分布足够均匀；dispatcherKey 通常是 uid/service 名，前若干字节已有足够熵）。
func (m *DispatchKeyStatsMiddleware) shardFor(key string) *dispatchKeyShard {
	const maxHashBytes = 32
	n := len(key)
	if n > maxHashBytes {
		n = maxHashBytes
	}
	h := uint32(2166136261)
	for i := 0; i < n; i++ {
		h ^= uint32(key[i])
		h *= 16777619
	}
	return &m.shards[h&(dispatchKeyShards-1)]
}

// Name 返回中间件名称
func (m *DispatchKeyStatsMiddleware) Name() string {
	return "DispatchKeyStats"
}

// OnStart 当 Mailbox 启动时调用
func (m *DispatchKeyStatsMiddleware) OnStart() {
	if m == nil || m.logger == nil {
		return
	}
	if !m.running.CompareAndSwap(false, true) {
		return
	}

	ticker := time.NewTicker(m.interval)
	go func() {
		defer ticker.Stop()
		defer close(m.doneCh) // 退出前通知 OnStop
		for {
			select {
			case <-ticker.C:
				m.reportAndReset()
			case <-m.stopCh:
				m.reportAndReset()
				return
			}
		}
	}()
}

// OnStop 当 Mailbox 停止时调用。
// 需等待后台 ticker goroutine 彻底退出后才返回，避免与外部“已停止”语义
// 不一致、防止 goroutine 在 reportAndReset 中访问已释放的 logger / shards。
func (m *DispatchKeyStatsMiddleware) OnStop() {
	if m == nil {
		return
	}
	m.stopOnce.Do(func() { close(m.stopCh) })
	// 仅当 OnStart 启动过 goroutine（running=true）才需要等待 doneCh。
	if m.running.Load() {
		<-m.doneCh
	}
}

// OnReceive 消息入队前调用，记录 dispatcherKey 统计
func (m *DispatchKeyStatsMiddleware) OnReceive(mctx inf.IMiddlewareContext) dto.MiddlewareResult {
	if m == nil || mctx == nil || mctx.Job() == nil {
		return dto.Continue()
	}
	key := mctx.Job().GetDispatcherKey()
	if key == "" {
		key = "<empty>"
	}

	s := m.shardFor(key)
	s.mu.Lock()
	// 如果 key 已存在，直接增加计数
	if _, exists := s.counts[key]; exists {
		s.counts[key]++
		s.total++
		s.mu.Unlock()
		return dto.Continue()
	}
	// 使用全局 key 总数判断是否达到上限（而非 per-shard 平均分配）
	if int(m.totalKeys.Load()) < m.maxKeys {
		s.counts[key] = 1
		s.total++
		m.totalKeys.Add(1)
	}
	s.mu.Unlock()
	// 达到上限，丢弃新 key 不记录（避免 O(n) 淘汰）
	return dto.Continue()
}

// OnComplete 消息处理完成后调用
func (m *DispatchKeyStatsMiddleware) OnComplete(mctx inf.IMiddlewareContext, err error, panicVal interface{}) {
	// 此中间件只在入队时统计，不需要后置处理
}

type keyCount struct {
	key   string
	count uint64
}

func (m *DispatchKeyStatsMiddleware) reportAndReset() {
	if m == nil || m.logger == nil {
		return
	}

	// 从所有分段收集并重置
	counts := make(map[string]uint64, 256)
	var total uint64
	for i := range m.shards {
		s := &m.shards[i]
		s.mu.Lock()
		for k, c := range s.counts {
			counts[k] += c
		}
		total += s.total
		s.counts = make(map[string]uint64, 64)
		s.total = 0
		s.mu.Unlock()
	}
	// 重置全局 key 计数器
	m.totalKeys.Store(0)

	if total == 0 {
		return
	}

	items := make([]keyCount, 0, len(counts))
	for k, c := range counts {
		items = append(items, keyCount{key: k, count: c})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].count > items[j].count })

	if len(items) > m.topN {
		items = items[:m.topN]
	}

	// Compact single-line summary.
	msg := "dispatchKey stats (interval): total=" + itoaU64(total) + " unique=" + itoa(len(counts)) + " top="
	for i, it := range items {
		if i > 0 {
			msg += ", "
		}
		pct := float64(it.count) * 100 / float64(total)
		msg += it.key + "=" + itoaU64(it.count) + "(" + formatPct(pct) + "%)"
	}

	m.logger.Info(msg)
}

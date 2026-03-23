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

// DispatchKeyStatsMiddleware is a debug helper that reports dispatcherKey hot spots.
// It counts keys over an interval and logs top-N periodically.
//
// Designed to be enabled only in debug mode.
// Implements IMailboxMiddleware interface.
type DispatchKeyStatsMiddleware struct {
	logger   log.ILoggerX
	interval time.Duration
	topN     int
	maxKeys  int

	stopOnce sync.Once
	stopCh   chan struct{}
	running  atomic.Bool

	mu     sync.Mutex
	counts map[string]uint64
	total  uint64
}

func NewDispatchKeyStatsMiddleware(logger log.ILoggerX, interval time.Duration, topN int) *DispatchKeyStatsMiddleware {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	if topN <= 0 {
		topN = 10
	}
	return &DispatchKeyStatsMiddleware{
		logger:   logger,
		interval: interval,
		topN:     topN,
		maxKeys:  100_000,
		stopCh:   make(chan struct{}),
		counts:   make(map[string]uint64, 1024),
	}
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

// OnStop 当 Mailbox 停止时调用
func (m *DispatchKeyStatsMiddleware) OnStop() {
	if m == nil {
		return
	}
	m.stopOnce.Do(func() { close(m.stopCh) })
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

	m.mu.Lock()
	defer m.mu.Unlock()

	// 如果 key 已存在，直接增加计数
	if _, exists := m.counts[key]; exists {
		m.counts[key]++
		m.total++
		return dto.Continue()
	}

	// 如果未达到上限，添加新 key
	if len(m.counts) < m.maxKeys {
		m.counts[key] = 1
		m.total++
		return dto.Continue()
	}

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

	m.mu.Lock()
	counts := m.counts
	total := m.total
	m.counts = make(map[string]uint64, 1024)
	m.total = 0
	m.mu.Unlock()

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

func itoa(n int) string {
	return itoaU64(uint64(n))
}

func itoaU64(n uint64) string {
	// tiny, allocation-free-ish integer formatting
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func formatPct(p float64) string {
	// keep it short: 1 decimal
	// (avoid fmt to reduce overhead in hot paths)
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	whole := int(p)
	frac := int((p - float64(whole)) * 10)
	return itoa(whole) + "." + itoa(frac)
}

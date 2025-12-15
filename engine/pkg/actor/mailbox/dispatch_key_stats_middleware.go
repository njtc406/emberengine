package mailbox

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
)

// DispatchKeyStatsMiddleware is a debug helper that reports dispatcherKey hot spots.
// It counts keys over an interval and logs top-N periodically.
//
// Designed to be enabled only in debug mode.
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

func (m *DispatchKeyStatsMiddleware) MailboxStarted() {
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

func (m *DispatchKeyStatsMiddleware) MessageReceived(ctx context.Context, evt inf.IEvent) {
	if m == nil || evt == nil {
		return
	}
	key := evt.GetDispatcherKey()
	if key == "" {
		key = "<empty>"
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Prevent unbounded growth in long-running debug sessions.
	if len(m.counts) >= m.maxKeys {
		m.total++
		return
	}

	m.counts[key]++
	m.total++
}

func (m *DispatchKeyStatsMiddleware) MessageProcessed(ctx context.Context, _ inf.IEvent) {}

// Close stops the background reporter.
func (m *DispatchKeyStatsMiddleware) Close() {
	if m == nil {
		return
	}
	m.stopOnce.Do(func() { close(m.stopCh) })
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

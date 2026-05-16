package authz

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// PolicyWatcher 负责策略的初始加载和持续 watch 更新。
// 初始加载失败时根据 failOpen 决定是否阻断启动。
type PolicyWatcher struct {
	store         PolicyStore
	authorizer    *Authorizer
	failOpen      bool
	retryInterval time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	once   sync.Once // 保证 Stop 幂等
}

// PolicyWatcherConfig 用于构造 PolicyWatcher。
type PolicyWatcherConfig struct {
	Store      PolicyStore
	Authorizer *Authorizer
	// FailOpen 控制初始策略加载失败时的行为。
	// false（默认）= fail-closed，加载失败则阻断启动，拒绝所有请求。
	// true = fail-open，加载失败仅记录警告，放行所有请求。
	// ⚠️ 生产环境强烈建议保持默认 fail-closed，否则攻击者可利用策略加载失败的窗口期绕过授权。
	FailOpen      bool
	RetryInterval time.Duration
}

// NewPolicyWatcher 创建 PolicyWatcher。
func NewPolicyWatcher(cfg PolicyWatcherConfig) *PolicyWatcher {
	if cfg.RetryInterval <= 0 {
		cfg.RetryInterval = 5 * time.Second
	}
	return &PolicyWatcher{
		store:         cfg.Store,
		authorizer:    cfg.Authorizer,
		failOpen:      cfg.FailOpen,
		retryInterval: cfg.RetryInterval,
	}
}

// Start 执行初始策略加载，并启动 watch 循环（如果 store 支持）。
// 初始加载失败时：failOpen=true 放行，failOpen=false 返回错误。
func (w *PolicyWatcher) Start(ctx context.Context) error {
	w.ctx, w.cancel = context.WithCancel(ctx)

	// 初始加载
	snapshot, err := w.store.Load(w.ctx)
	if err != nil {
		if !w.failOpen {
			return fmt.Errorf("authz: initial policy load failed (fail-closed): %w", err)
		}
		// fail-open: 日志警告但不阻断启动，授权引擎保持当前状态
	} else {
		if applyErr := w.authorizer.ApplySnapshot(snapshot); applyErr != nil {
			if !w.failOpen {
				return fmt.Errorf("authz: initial policy apply failed (fail-closed): %w", applyErr)
			}
		}
	}

	// 启动 watch 循环
	ch, watchErr := w.store.Watch(w.ctx)
	if watchErr != nil {
		return fmt.Errorf("authz: watch setup failed: %w", watchErr)
	}
	if ch != nil {
		w.wg.Add(1)
		go w.watchLoop(ch)
	}

	return nil
}

// Stop 幂等停止 watcher。
func (w *PolicyWatcher) Stop() {
	w.once.Do(func() {
		if w.cancel != nil {
			w.cancel()
		}
		w.wg.Wait()
		_ = w.store.Close()
	})
}

func (w *PolicyWatcher) watchLoop(ch <-chan PolicyEvent) {
	defer w.wg.Done()
	for {
		select {
		case <-w.ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			w.handleEvent(evt)
		}
	}
}

func (w *PolicyWatcher) handleEvent(evt PolicyEvent) {
	switch evt.Type {
	case PolicyEventUpdate:
		if evt.Snapshot != nil {
			// 校验失败保留旧快照
			_ = w.authorizer.ApplySnapshot(evt.Snapshot)
		}
	case PolicyEventDelete:
		// 策略被删除，保留旧快照不清空（安全默认）
	case PolicyEventError:
		// watch 错误，保留旧快照
	}
}

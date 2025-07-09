// Package ebCtx
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/8 0008 23:16
// 最后更新:  yr  2025/7/8 0008 23:16
package ebCtx

import (
	"context"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/emberctx"
	"sync"
	"time"
)

// EContext is a context wrapper that supports headers and cancellation.
// It is not safe for concurrent use.
type EContext struct {
	lock            sync.RWMutex
	context.Context                    // the actual context
	cancel          context.CancelFunc // cancel for the inner context
}

// NewEContext creates a new EContext with background context.
func NewEContext() *EContext {
	return &EContext{
		Context: emberctx.NewCtx(context.Background()),
	}
}

// NewEContextWithCancel creates a new EContext with background context and cancel.
func NewEContextWithCancel() *EContext {
	ctx, cancel := context.WithCancel(context.Background())
	return &EContext{
		Context: emberctx.NewCtx(ctx),
		cancel:  cancel,
	}
}

// SetHeaders sets multiple headers.
func (e *EContext) SetHeaders(headers dto.Header) inf.IContext {
	if headers != nil {
		e.lock.Lock()
		defer e.lock.Unlock()
		if e.Context == nil {
			e.Context, e.cancel = context.WithCancel(context.Background())
		}

		e.Context = emberctx.AddHeaders(e.Context, headers)
	}
	return e
}

// SetHeader sets a single header.
func (e *EContext) SetHeader(key, value string) inf.IContext {
	e.lock.Lock()
	defer e.lock.Unlock()
	if e.Context == nil {
		e.Context, e.cancel = context.WithCancel(context.Background())
	}

	e.Context = emberctx.AddHeader(e.Context, key, value)
	return e
}

// GetHeader gets a header value.
func (e *EContext) GetHeader(key string) string {
	e.lock.RLock()
	defer e.lock.RUnlock()
	if e.Context == nil {
		return ""
	}

	return emberctx.GetHeaderValue(e.Context, key)
}

// GetHeaders returns all headers.
func (e *EContext) GetHeaders() dto.Header {
	e.lock.RLock()
	defer e.lock.RUnlock()
	if e.Context == nil {
		return nil
	}
	return emberctx.GetHeader(e.Context)
}

// WithContext replaces the inner context (e.g., for timeouts).
// Note: This does not cancel the previous context.
func (e *EContext) WithContext(ctx context.Context) inf.IContext {
	e.lock.Lock()
	defer e.lock.Unlock()

	e.reset()

	e.Context = ctx
	// We don't cancel the old context because it might be shared
	return e
}

func (e *EContext) WithTimeout(timeout time.Duration) inf.IContext {
	e.lock.Lock()
	defer e.lock.Unlock()

	e.reset()

	e.Context, e.cancel = context.WithTimeout(e.Context, timeout)
	return e
}

func (e *EContext) WithCancelContext(ctx context.Context, cancel context.CancelFunc) inf.IContext {
	e.lock.Lock()
	defer e.lock.Unlock()
	e.reset()
	e.Context = ctx
	e.cancel = cancel
	return e
}

func (e *EContext) GetContext() inf.IContext {
	return e
}

func (e *EContext) GetBaseContext() context.Context {
	return e.Context
}

func (e *EContext) Done() <-chan struct{} {
	if e.Context == nil {
		return nil
	}
	return e.Context.Done()
}

// SetDone cancels the context if it was created by us.
func (e *EContext) SetDone() {
	if e.cancel != nil {
		e.cancel()
	}
}

// Reset resets the context to background and clears headers.
func (e *EContext) Reset() {
	e.reset()
	e.Context, e.cancel = context.WithCancel(context.Background())
}

func (e *EContext) reset() {
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
}

func (e *EContext) GetTranceId() string {
	return e.GetHeader(def.DefaultTraceIdKey)
}

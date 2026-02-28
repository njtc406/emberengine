// Package asynclib
// 协程池封装：提供 Node 级独立协程池实例，替代原来的全局 antsPool。
package asynclib

import (
	"fmt"

	"github.com/panjf2000/ants/v2"
)

// Pool 是 ants 协程池的实例级封装。
// 每个 Node 持有独立的 Pool，互不干扰。
type Pool struct {
	inner *ants.Pool
}

// NewPool 创建一个新的协程池实例。
// size 为池容量（必须 > 0），创建失败返回 error 而非 panic。
func NewPool(size int, options ...ants.Option) (*Pool, error) {
	if size <= 0 {
		return nil, fmt.Errorf("asynclib.NewPool: size must be > 0, got %d", size)
	}
	opts := append([]ants.Option{ants.WithPreAlloc(true)}, options...)
	p, err := ants.NewPool(size, opts...)
	if err != nil {
		return nil, fmt.Errorf("asynclib.NewPool: %w", err)
	}
	return &Pool{inner: p}, nil
}

// Go 提交一个任务到协程池执行。
func (p *Pool) Go(f func()) error {
	if p == nil || p.inner == nil {
		return fmt.Errorf("asynclib.Pool: pool is nil")
	}
	return p.inner.Submit(f)
}

// Release 释放协程池资源。
func (p *Pool) Release() {
	if p != nil && p.inner != nil {
		p.inner.Release()
		p.inner = nil
	}
}

// Running 返回当前正在运行的 goroutine 数量。
func (p *Pool) Running() int {
	if p == nil || p.inner == nil {
		return 0
	}
	return p.inner.Running()
}

// Cap 返回池容量。
func (p *Pool) Cap() int {
	if p == nil || p.inner == nil {
		return 0
	}
	return p.inner.Cap()
}

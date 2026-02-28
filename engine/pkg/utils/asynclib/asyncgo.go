// Package asynclib
// 异步执行：使用协程池中的协程执行任务,防止出现瞬间创建大量协程,出现性能问题。
//
// 本文件中的全局函数已废弃，仅为尚未完成 Node 化改造的消费方提供临时兼容。
// 后续 Phase 中会逐步移除。
package asynclib

// Deprecated: 以下全局函数仅供向后兼容。新代码请使用 Pool 实例方法。
// 这些将在所有消费方迁移到 INodeContext 后删除。

var defaultPool *Pool // 临时全局池，由 Node.Start 设置

// SetDefaultPool 由 Node.Start 调用，设置全局默认池（临时兼容）。
// Deprecated: 后续 Phase 删除。
func SetDefaultPool(p *Pool) { defaultPool = p }

// Go 提交任务到默认全局池。
// Deprecated: 请通过 NodeContext.AntsPool().Go(f) 替代。
func Go(f func()) error {
	if defaultPool == nil {
		return ErrNoDefaultPool
	}
	return defaultPool.Go(f)
}

// Release 释放默认全局池。
// Deprecated: 请通过 Pool.Release() 替代。
func Release() {
	if defaultPool != nil {
		defaultPool.Release()
		defaultPool = nil
	}
}

var ErrNoDefaultPool = &poolError{"asynclib: default pool not initialized"}

type poolError struct{ msg string }

func (e *poolError) Error() string { return e.msg }

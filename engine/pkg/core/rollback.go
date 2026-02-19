package core

// TxHookFunc 事务钩子函数。
// 钩子内部自行根据脏标记等条件判断是否需要执行实际操作。
type TxHookFunc func()

// TxHookManager 事务钩子管理器，提供类事务的 commit/rollback 语义。
//
// 在 Service.Init 阶段一次性注册 commit 和 rollback 两组钩子，后续不再修改。
// 每次写操作(Write) Job 执行完成后：
//   - handler 成功 → 框架按 LIFO 顺序执行所有 commit 钩子
//   - handler 失败 → 框架按 LIFO 顺序执行所有 rollback 钩子
//
// 此时 Job 仍存活、外层 rwMu 锁仍持有，业务状态可安全操作。
// 各钩子内部自行根据脏标记决定是否需要执行实际操作。
//
// 注意：仅在写操作(Write)路径下执行。读操作(Read)路径并发执行，
// 不应修改共享状态，因此跳过事务钩子。
type TxHookManager struct {
	commits   []TxHookFunc
	rollbacks []TxHookFunc
}

// RegisterCommit 注册一个 commit 钩子（Init 阶段调用，LIFO 顺序执行）
func (tm *TxHookManager) RegisterCommit(fn TxHookFunc) {
	if fn != nil {
		tm.commits = append(tm.commits, fn)
	}
}

// RegisterRollback 注册一个 rollback 钩子（Init 阶段调用，LIFO 顺序执行）
func (tm *TxHookManager) RegisterRollback(fn TxHookFunc) {
	if fn != nil {
		tm.rollbacks = append(tm.rollbacks, fn)
	}
}

// Commit 按 LIFO 顺序执行所有已注册的 commit 钩子。
// 每个钩子单独 panic 保护，不会因某个钩子失败而中断后续执行。
func (tm *TxHookManager) Commit() (panics int) {
	return execHooks(tm.commits)
}

// Rollback 按 LIFO 顺序执行所有已注册的 rollback 钩子。
// 每个钩子单独 panic 保护，不会因某个钩子失败而中断后续执行。
func (tm *TxHookManager) Rollback() (panics int) {
	return execHooks(tm.rollbacks)
}

// HasCommit 是否注册了 commit 钩子
func (tm *TxHookManager) HasCommit() bool {
	return len(tm.commits) > 0
}

// HasRollback 是否注册了 rollback 钩子
func (tm *TxHookManager) HasRollback() bool {
	return len(tm.rollbacks) > 0
}

// execHooks 按 LIFO 顺序执行钩子列表，每个钩子单独 panic 保护
func execHooks(hooks []TxHookFunc) (panics int) {
	for i := len(hooks) - 1; i >= 0; i-- {
		func() {
			defer func() {
				if recover() != nil {
					panics++
				}
			}()
			hooks[i]()
		}()
	}
	return
}

package job

import "github.com/njtc406/emberengine/engine/pkg/def"

// resetFactoryFrozenForTest resets the frozen flag for testing purposes only.
func resetFactoryFrozenForTest() {
	jobFactoryFrozen.Store(false)
}

// unregisterFactoryForTest 删除指定 jobType 的注册记录，仅供测试 cleanup 使用，
// 避免 -count=N 重跑时残留前一轮注册导致 "already registered" 报错。
func unregisterFactoryForTest(jobType def.MailboxJobType) {
	jobFactoryMu.Lock()
	defer jobFactoryMu.Unlock()
	delete(jobFactory, jobType)
}

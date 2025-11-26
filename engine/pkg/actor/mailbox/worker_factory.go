// Package mailbox
// 模块名: 工作线程工厂
// 功能描述: 创建Worker实例
// 作者:  yr  2025/11/27
// 最后更新:  yr  2025/11/27
package mailbox

import (
	"github.com/njtc406/emberengine/engine/pkg/config"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
)

// workerFactory Worker工厂函数映射
var workerFactory = map[string]func(workerId int, conf *config.MailboxConf, pool *WorkerPool) inf.IMailboxWorker{
	"unified": NewWorker, // 统一Worker（推荐）
}

// RegisterWorkerFactory 注册自定义Worker工厂函数
func RegisterWorkerFactory(name string, fun func(workerId int, conf *config.MailboxConf, pool *WorkerPool) inf.IMailboxWorker) {
	workerFactory[name] = fun
}

// newWorker 创建Worker实例
// 根据配置选择合适的Worker实现，默认使用统一Worker
func newWorker(workerId int, conf *config.MailboxConf, pool *WorkerPool) inf.IMailboxWorker {
	// 检查是否有自定义工厂（用于扩展）
	workerType := "unified" // 默认使用统一Worker
	if fun, ok := workerFactory[workerType]; ok {
		return fun(workerId, conf, pool)
	}

	// 降级：直接创建统一Worker
	log.SysLogger.Warnf("Unknown worker type: %s, using unified worker", workerType)
	return NewWorker(workerId, conf, pool)
}

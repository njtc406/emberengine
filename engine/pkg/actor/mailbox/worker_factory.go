// Package mailbox
// 模块名: 工作线程工厂
// 功能描述: 描述
// 作者:  yr  2025/11/22 0022 0:34
// 最后更新:  yr  2025/11/22 0022 0:34
package mailbox

import (
	"github.com/njtc406/emberengine/engine/pkg/config"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

var factory = map[string]func(workerId int, conf *config.MailboxConf, pool *WorkerPool) inf.IMailboxWorker{
	"simple": newSimpleWorker,
	"multi":  newMultiWorker,
}

func RegisterWorkerFactory(name string, fun func(workerId int, conf *config.MailboxConf, pool *WorkerPool) inf.IMailboxWorker) {
	factory[name] = fun
}

func newWorker(workerId int, conf *config.MailboxConf, pool *WorkerPool) inf.IMailboxWorker {
	if fun, ok := factory[conf.MailboxType]; ok {
		return fun(workerId, conf, pool)
	}
	return nil
}

// Package mailbox
// @Title  双队列工作线程
// @Description  desc
// @Author  yr  2025/2/8
// @Update  yr  2025/2/8
package mailbox

import (
	"fmt"
	"reflect"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/profiler"
	"github.com/njtc406/emberengine/engine/pkg/utils/backoff"
	"github.com/njtc406/emberengine/engine/pkg/utils/idle"
	"github.com/njtc406/emberengine/engine/pkg/utils/mpsc"
)

type DefaultWorker struct {
	workerId      int
	closed        atomic.Bool
	pool          *WorkerPool
	wg            sync.WaitGroup
	userMailbox   queue[inf.IEvent] // 用户消息
	systemMailbox queue[inf.IEvent] // 系统消息(高优先级)
	idler         *idle.Controller
}

func (w *DefaultWorker) GetWorkerId() int {
	return w.workerId
}

func newDefaultWorker(workerId int, conf *config.MailboxConf, pool *WorkerPool) inf.IMailboxWorker {
	w := &DefaultWorker{
		workerId:      workerId,
		pool:          pool,
		userMailbox:   mpsc.New[inf.IEvent](),
		systemMailbox: mpsc.New[inf.IEvent](),
	}
	w.idler = idle.NewController(conf.DefaultConf.BackoffBaseDelay, conf.DefaultConf.BackoffMaxDelay, conf.DefaultConf.MaxIdleBeforeBackoff, conf.DefaultConf.BackoffMaxRetries)
	return w
}

func (w *DefaultWorker) SubmitEvent(e inf.IEvent) error {
	if w.isClosed() {
		return def.ErrMailboxWorkerClosed
	}
	if w.userMailbox == nil {
		return def.ErrMailboxWorkerChannelNotInit
	}

	// 只区分普通消息和系统消息
	if e.GetPriority() < def.PriorityNormal {
		w.systemMailbox.Push(e)
	} else {
		w.userMailbox.Push(e)
	}

	return nil
}

func (w *DefaultWorker) Start() {
	w.wg.Add(1)
	go w.run()
}

func (w *DefaultWorker) run() {
	//log.SysLogger.Debugf("worker %d start", w.workerId)
	defer w.wg.Done()

	var e inf.IEvent
	var ok bool
	defer func() {
		// 退出时检查业务是否处理完成
		for !w.systemMailbox.Empty() {
			if e, ok = w.systemMailbox.Pop(); ok {
				w.safeExec(w.pool.invoker.InvokeMessage, e)
			}
		}

		for !w.userMailbox.Empty() {
			if e, ok = w.userMailbox.Pop(); ok {
				w.safeExec(w.pool.invoker.InvokeMessage, e)
			}
		}
	}()

	for !w.closed.Load() {
		// 优先处理系统消息
		if e, ok = w.systemMailbox.Pop(); ok {
			if w.idler != nil {
				w.idler.Reset()
			} else {
				w.backoff.Reset()
			}
			w.safeExec(w.pool.invoker.InvokeMessage, e)
			continue
		}

		if e, ok = w.userMailbox.Pop(); ok {
			if w.idler != nil {
				w.idler.Reset()
			} else {
				w.backoff.Reset()
			}
			// 交由业务处理消息
			w.safeExec(w.pool.invoker.InvokeMessage, e)
			continue
		}

		// 使用通用 idle 控制器来处理空闲退避策略
		if w.idler != nil {
			w.idler.Idle()
		} else {
			// 保底兼容：如果 idler 为空，仍然保持原有 sleep(backoff.NextDelay()) 逻辑
			time.Sleep(w.backoff.NextDelay())
		}

		//runtime.Gosched()
	}
	//log.SysLogger.Debugf("worker %d stopped", w.workerId)
}

func (w *DefaultWorker) Stop() {
	//log.SysLogger.Debugf("worker %d process userCount:%d  sysCount:%d", w.workerId, w.userCount.Load(), w.sysCount.Load())
	if !w.closed.CompareAndSwap(false, true) {
		return
	}
	w.wg.Wait()
}

func (w *DefaultWorker) isClosed() bool {
	return w.closed.Load()
}

func (w *DefaultWorker) safeExec(invokeFun func(inf.IEvent), e inf.IEvent) {
	defer func() {
		if r := recover(); r != nil {
			log.SysLogger.Errorf("exec error: %v\ntrace:%s", r, debug.Stack())
			func() {
				defer func() {
					if r2 := recover(); r2 != nil {
						log.SysLogger.Errorf("EscalateFailure also panicked: %v\ntrace:%s", r2, debug.Stack())
					}
				}()
				w.pool.invoker.EscalateFailure(r, e)
			}()
		}
	}()

	var analyzer *profiler.Analyzer
	if w.pool.profiler != nil {
		analyzer = w.pool.profiler.Push(fmt.Sprintf("[ STATE ]%s", reflect.TypeOf(e).String()))
	}
	invokeFun(e)
	if analyzer != nil {
		analyzer.Pop()
		analyzer = nil
	}

	for _, ms := range w.pool.middlewares {
		ms.MessageReceived(e)
	}
}

func (w *DefaultWorker) GetMsgLen() int {
	var msgLen int
	if w.userMailbox != nil {
		msgLen += w.userMailbox.Len()
	}
	if w.systemMailbox != nil {
		msgLen += w.systemMailbox.Len()
	}
	return msgLen
}

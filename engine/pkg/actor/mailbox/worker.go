// Package mailbox
// @Title  服务的工作线程,接收并处理事件
// @Description  desc
// @Author  yr  2025/2/8
// @Update  yr  2025/2/8
package mailbox

import (
	"fmt"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/utils/mpsc"
	"reflect"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/profiler"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
)

type Worker struct {
	workerId      int
	closed        bool
	pool          *WorkerPool
	wg            sync.WaitGroup
	userMailbox   queue[inf.IEvent] // 用户消息
	systemMailbox queue[inf.IEvent] // 系统消息(区分出不同级别的消息,这样可以使用系统消息来控制service行为,同时也保证系统消息有更高的执行优先级)
	userCount     atomic.Int64
	sysCount      atomic.Int64
}

func newWorker(pool *WorkerPool, id int) *Worker {
	return &Worker{
		workerId:      id,
		pool:          pool,
		userMailbox:   mpsc.New[inf.IEvent](),
		systemMailbox: mpsc.New[inf.IEvent](),
	}
}

func (w *Worker) submitUserEvent(e inf.IEvent) error {
	if w.userMailbox == nil {
		return def.ErrMailboxWorkerUserChannelNotInit
	}
	w.userCount.Add(1)
	if !w.userMailbox.Push(e) {
		return nil //def.ErrEventChannelIsFull
	}
	return nil
}

func (w *Worker) submitSysEvent(e inf.IEvent) error {
	if w.systemMailbox == nil {
		return def.ErrMailboxWorkerSysChannelNotInit
	}
	w.sysCount.Add(1)
	if !w.systemMailbox.Push(e) {
		return def.ErrSysEventChannelIsFull
	}
	return nil
}

func (w *Worker) Start() {
	w.wg.Add(1)
	go w.run()
}

func (w *Worker) run() {
	//log.SysLogger.Debugf("worker %d start", w.workerId)
	defer w.wg.Done()

	var e inf.IEvent
	var ok bool

	// 这里暂时先屏蔽,后面已经处理过panic了
	//defer func() {
	//	if r := recover(); r != nil {
	//		w.invoker.EscalateFailure(r, e)
	//	}
	//	// 重启listen
	//	w.wg.Add(1)
	//	go w.listen()
	//}()

	defer func() {
		// 退出时检查业务是否处理完成
		for !w.systemMailbox.Empty() {
			if e, ok = w.systemMailbox.Pop(); ok {
				w.safeExec(w.pool.invoker.InvokeSystemMessage, e)
			}
		}

		for !w.userMailbox.Empty() {
			if e, ok = w.userMailbox.Pop(); ok {
				w.safeExec(w.pool.invoker.InvokeUserMessage, e)
			}
		}
	}()

	var backoff = 1
	var maxBackoff = 4
	for !w.closed {
		// 优先处理系统消息
		if e, ok = w.systemMailbox.Pop(); ok {
			w.safeExec(w.pool.invoker.InvokeSystemMessage, e)
			continue
		}

		if e, ok = w.userMailbox.Pop(); ok {
			// 交由业务处理消息
			w.safeExec(w.pool.invoker.InvokeUserMessage, e)
			continue
		}

		// 使用指数退避来减少忙等开销
		if backoff < maxBackoff {
			backoff *= 2
		}
		time.Sleep(time.Microsecond * time.Duration(backoff))

		//runtime.Gosched()
	}
	//log.SysLogger.Debugf("worker %d stopped", w.workerId)
}

func (w *Worker) stop() {
	//log.SysLogger.Debugf("worker %d process userCount:%d  sysCount:%d", w.workerId, w.userCount.Load(), w.sysCount.Load())
	w.closed = true
	w.wg.Wait()
	w.userMailbox = nil
	w.systemMailbox = nil
	w.pool = nil
	w.workerId = 0
}

func (w *Worker) safeExec(invokeFun func(inf.IEvent), e inf.IEvent) {
	defer func() {
		if r := recover(); r != nil {
			log.SysLogger.Errorf("exec error: %v\ntrace:%s", r, debug.Stack())
			w.pool.invoker.EscalateFailure(r, e)
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

func (w *Worker) GetMsgLen() int {
	if w.userMailbox == nil {
		return 0
	}
	return w.userMailbox.Len()
}

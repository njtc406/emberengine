package concurrent

import (
	"github.com/njtc406/emberengine/engine/utils/log"
	"runtime/debug"
	"sync"
)

type task struct {
	queueId int64
	fn      func() bool
	cb      func(err error)
}

type worker struct {
	*dispatch
}

func (t *task) isExistTask() bool {
	return t.fn == nil
}

func (w *worker) start(waitGroup *sync.WaitGroup, t *task, d *dispatch) {
	w.dispatch = d
	d.workerNum += 1
	waitGroup.Add(1)
	go w.run(waitGroup, *t)
}

func (w *worker) run(waitGroup *sync.WaitGroup, t task) {
	defer waitGroup.Done()

	w.exec(&t)
	for {
		select {
		case tw := <-w.workerQueue:
			if tw.isExistTask() {
				//exit goroutine
				log.SysLogger.Info("worker goroutine exit")
				return
			}
			w.exec(&tw)
		}
	}
}

func (w *worker) exec(t *task) {
	defer func() {
		if r := recover(); r != nil {
			cb := t.cb
			t.cb = func(err error) {
				if cb != nil {
					cb(r.(error))
				}
			}
			log.SysLogger.Errorf("errdef:%s\ntrace:%s", r, debug.Stack())
			w.endCallFun(true, t)
		}
	}()

	w.endCallFun(t.fn(), t)
}

func (w *worker) endCallFun(isDoCallBack bool, t *task) {
	if isDoCallBack {
		w.pushAsyncDoCallbackEvent(t.cb)
	}

	if t.queueId != 0 {
		w.pushQueueTaskFinishEvent(t.queueId)
	}
}

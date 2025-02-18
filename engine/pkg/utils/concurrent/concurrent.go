// Package concurrent
// @Title  并发模块
// @Description  desc
// @Author  yr  2025/2/18
// @Update  yr  2025/2/18
package concurrent

import (
	"fmt"
	"github.com/njtc406/emberengine/engine/pkg/utils/asynclib"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/panjf2000/ants/v2"
)

type IConcurrent interface {
	OpenConcurrent(poolSize, callbackChannelSize int)
	AsyncDo(f func() error, cb func(err error))
	GetChannel() chan IConcurrentCallback
	Close()
}

type IConcurrentCallback interface {
	DoCallback()
}

// TaskScheduler 是并发任务调度器
type TaskScheduler struct {
	pool *ants.Pool
	c    chan IConcurrentCallback
}

// NewTaskScheduler 创建一个新的任务调度器
func NewTaskScheduler() IConcurrent {
	return &TaskScheduler{}
}

// OpenConcurrent 初始化并发调度器
func (s *TaskScheduler) OpenConcurrent(poolSize, callbackChannelSize int) {
	if s.pool == nil {
		s.pool = asynclib.NewAntsPool(poolSize)
	}
	if s.c == nil {
		s.c = make(chan IConcurrentCallback, callbackChannelSize)
	}
}

func (s *TaskScheduler) GetChannel() chan IConcurrentCallback {
	return s.c
}

// AsyncDo 添加一个任务到调度器
func (s *TaskScheduler) AsyncDo(fn func() error, cb func(error)) {
	if s.pool == nil || (fn == nil && cb == nil) {
		return
	}
	task := &Task{
		fn:       fn,
		callback: cb,
	}

	if fn == nil && cb != nil {
		// 只有回调函数,相当于下一帧执行一个指定函数
		s.notifyCallback(cb, nil)
		return
	}

	err := s.pool.Submit(func() {
		defer func() {
			if r := recover(); r != nil {
				if err, ok := r.(error); ok {
					task.err = err
				} else {
					task.err = fmt.Errorf("panic: %v", r)
				}
			}
			if task.callback != nil {
				s.notifyCallback(task.callback, task.err)
			}
		}()

		task.err = fn()
	})
	if err != nil {
		// 任务提交失败,直接回调
		s.notifyCallback(cb, err)
	}

}

func (s *TaskScheduler) notifyCallback(cb func(error), err error) {
	if cb == nil {
		return
	}
	if s.c != nil {
		select {
		case s.c <- &CallbackEvent{cb: cb, err: err}:
		default:
			// 通道满了时，可根据需要记录日志或采取其他措施
			log.SysLogger.Errorf("callback channel full or closed")
		}
	}
}

func (s *TaskScheduler) Close() {
	if s.c != nil {
		close(s.c)
		s.c = nil
	}

	if s.pool != nil {
		s.pool.Release()
		s.pool = nil
	}
}

// Task 表示一个并发任务(如果后续有大量并发任务,那么这里就改用pool创建)
type Task struct {
	fn       func() error
	callback func(error)
	err      error
}

// CallbackEvent 表示一个回调事件
type CallbackEvent struct {
	cb  func(error)
	err error
}

func (c *CallbackEvent) DoCallback() {
	if c.cb != nil {
		c.cb(c.err)
	}
}

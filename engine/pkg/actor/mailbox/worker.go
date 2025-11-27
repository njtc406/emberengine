// Package mailbox
// @Title  统一Worker实现
// @Description  统一的消息处理Worker，支持双队列和多优先级队列两种模式
// @Author  yr  2025/11/27
// @Update  yr  2025/11/27
package mailbox

import (
	"fmt"
	"reflect"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/profiler"
	"github.com/njtc406/emberengine/engine/pkg/utils/idle"
)

const (
	QueueModeDual     = "dual"
	QueueModePriority = "priority"
)

// Worker 统一的消息处理Worker
// 职责：处理消息的提交、调度、执行，支持双队列和多优先级队列两种模式
type Worker struct {
	workerId     int
	closed       atomic.Bool
	pool         *WorkerPool
	wg           sync.WaitGroup
	queueManager IQueueManager            // 队列管理器（可以是双队列或多优先级队列）
	idler        *idle.AdaptiveController // 自适应空闲控制器
	count        atomic.Int64
}

// NewWorker 创建统一Worker
func NewWorker(workerId int, conf *config.MailboxConf, pool *WorkerPool) inf.IMailboxWorker {
	w := &Worker{
		workerId: workerId,
		pool:     pool,
	}

	// 根据配置创建队列管理器
	w.queueManager = createQueueManager(conf)

	// 创建空闲控制器
	idlerConf := conf.SchedulePolicy.IdlerConf
	if idlerConf == nil {
		idlerConf = &config.WorkerIdlerConf{
			EnableCond: true,
		}
	}
	w.idler = idle.NewAdaptiveController(
		idlerConf.EnableCond,
		idlerConf.BackoffBaseDelay,
		idlerConf.BackoffMaxDelay,
		idlerConf.MaxIdleBeforeBackoff,
		idlerConf.BackoffMaxRetries,
	)

	return w
}

// createQueueManager 根据配置创建队列管理器
func createQueueManager(conf *config.MailboxConf) IQueueManager {
	// 确定队列模式
	queueMode := conf.QueueMode
	if queueMode == "" {
		queueMode = QueueModeDual // 默认双队列模式
	}

	switch queueMode {
	case QueueModeDual:
		// 双队列模式（系统队列 + 用户队列）
		return NewDualQueueManager()

	case QueueModePriority:
		// 多优先级队列模式
		var queueConf *config.MultiLevelQueueConf

		// 优先使用新配置
		if conf.SchedulePolicy != nil && conf.SchedulePolicy.MultiLevelQueueConf != nil {
			queueConf = conf.SchedulePolicy.MultiLevelQueueConf
		} else if conf.SchedulePolicy != nil && conf.SchedulePolicy.MultiLevelConf != nil {
			// 兼容旧配置
			old := conf.SchedulePolicy.MultiLevelConf
			queueConf = &config.MultiLevelQueueConf{
				Strategy:        old.Strategy,
				TotalBatchLimit: old.TotalBatchLimit,
				PriorityBatches: old.PriorityBatches,
			}
		} else {
			// 使用默认配置
			queueConf = DefaultMultiLevelQueueConf()
		}

		return NewPriorityQueueManager(queueConf)

	default:
		log.SysLogger.Warnf("Unknown queue mode: %s, using dual queue", queueMode)
		return NewDualQueueManager()
	}
}

// GetWorkerId 获取Worker的ID
func (w *Worker) GetWorkerId() int {
	return w.workerId
}

// SubmitEvent 提交事件到队列
func (w *Worker) SubmitEvent(e inf.IEvent) error {
	if w.closed.Load() {
		return def.ErrMailboxWorkerClosed
	}

	if w.queueManager == nil {
		return def.ErrMailboxWorkerChannelNotInit
	}

	// 提交到队列管理器
	err := w.queueManager.Submit(e)
	if err != nil {
		return err
	}
	// 增加事件计数
	w.count.Add(1)

	// 唤醒Worker
	if w.idler != nil {
		w.idler.Wake()
	}

	return nil
}

// Start 启动Worker
func (w *Worker) Start() {
	w.wg.Add(1)
	go w.run()
}

// run Worker主循环
func (w *Worker) run() {
	defer w.wg.Done()

	// 退出时处理所有剩余消息
	defer func() {
		w.queueManager.DrainAll(func(e inf.IEvent) {
			w.safeExec(e)
		})
	}()
	//var backoff = 1
	//var maxBackoff = 4
	// 主处理循环
	for !w.closed.Load() {
		// 尝试获取下一个事件
		if e, ok := w.queueManager.NextEvent(); ok {
			w.safeExec(e)
			continue
		}

		// 队列为空，使用空闲控制器等待
		w.idler.Idle()
		//if backoff < maxBackoff {
		//	backoff *= 2
		//}
		//time.Sleep(time.Microsecond * time.Duration(backoff))
	}
}

// Stop 停止Worker
func (w *Worker) Stop() {
	// 标记为已关闭
	if !w.closed.CompareAndSwap(false, true) {
		return // 已经关闭过了
	}

	// 唤醒可能在等待的Worker
	if w.idler != nil {
		w.idler.Wake()
	}

	// 等待Worker完全退出
	w.wg.Wait()
	// 打印计数
	log.SysLogger.Infof("Worker %d processed %d events", w.workerId, w.count.Load())
}

// safeExec 安全执行事件处理
func (w *Worker) safeExec(e inf.IEvent) {
	defer func() {
		if r := recover(); r != nil {
			log.SysLogger.WithContext(e.GetContext()).Errorf("exec error: %v\ntrace:%s", r, debug.Stack())

			// 双重保护：EscalateFailure 可能也会 panic
			func() {
				defer func() {
					if r2 := recover(); r2 != nil {
						log.SysLogger.WithContext(e.GetContext()).Errorf("EscalateFailure also panicked: %v\ntrace:%s", r2, debug.Stack())
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

	// 调用消息处理器
	w.pool.invoker.InvokeMessage(e)

	if analyzer != nil {
		analyzer.Pop()
		analyzer = nil
	}

	// 调用中间件
	for _, ms := range w.pool.middlewares {
		ms.MessageReceived(e)
	}
}

// GetMsgLen 获取队列中的消息总数
func (w *Worker) GetMsgLen() int {
	if w.queueManager == nil {
		return 0
	}
	return w.queueManager.GetMsgLen()
}

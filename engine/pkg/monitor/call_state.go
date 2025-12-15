package monitor

import (
	"context"
	"sync"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
	"github.com/njtc406/emberengine/engine/pkg/utils/xcontext"
)

// CallState 承载一次 RPC 调用（Call/AsyncCall）的等待/回调状态。
//
// 设计目标：
// - 跨 goroutine 只流转 CallState（轻量、无锁，靠消息队列/chan 同步）；
// - envelope 仅作为 mailbox 内部传输对象，不再作为“等待载体”。
//
// 生命周期：
// - Call：调用方持有 state，等待 done 后读取结果并 Release。
// - AsyncCall：state 由 monitor 持有；完成后投递到调用方 mailbox 执行回调，并在回调末尾 Release。
// - 超时：同完成逻辑。
//
// 注意：done 使用缓冲 channel（size=1）+ drain reset，避免 close 带来的不可复用问题。
//
// noinspection GoVetCopyLock
// (CallState 不包含锁，只包含轻量字段)
type CallState struct {
	dto.DataRef
	name string
	xcontext.XContext
	reqID      uint64
	timerID    uint64
	timeout    time.Duration // nanoseconds
	method     string
	dispatcher inf.IRpcDispatcher
	callbacks  []dto.CompletionFunc
	cbParams   []interface{}
	done       chan struct{}
	resp       interface{}
	err        error
}

var callStatePool pool.IPool[*CallState]
var callStatePoolOnce sync.Once

func getCallStatePool() pool.IPool[*CallState] {
	callStatePoolOnce.Do(func() {
		callStatePool = pool.NewSyncPoolWrapper(
			func() *CallState {
				return &CallState{}
			},
			func() pool.IStatsRecorder {
				if config.IsDebug() {
					return pool.NewStatsRecorder("rpcCallStatePool")
				} else {
					return pool.NewNoStatsRecorder()
				}
			}(),
			pool.WithRef(func(s *CallState) { s.Ref() }),
			pool.WithUnRef(func(s *CallState) bool { return s.UnRef() }),
			pool.WithReset(func(s *CallState) { s.Reset() }),
		)
	})
	return callStatePool
}

func newCallState() *CallState {
	return getCallStatePool().Get()
}

func (s *CallState) Reset() {
	s.name = ""
	s.XContext.Reset()
	s.reqID = 0
	s.timerID = 0
	s.timeout = 0
	s.method = ""
	s.dispatcher = nil
	s.callbacks = s.callbacks[:0]
	s.cbParams = s.cbParams[:0]
	s.resp = nil
	s.err = nil
	if s.done == nil {
		s.done = make(chan struct{}, 1)
	}
	if len(s.done) > 0 {
		<-s.done
	}
}

func (s *CallState) SetName(name string) { s.name = name }
func (s *CallState) GetName() string {
	if s.name != "" {
		return s.name
	}
	if s.method != "" {
		return "rpc-cb:" + s.method
	}
	return "rpc-cb"
}

func NewCallState(ctx context.Context, reqID uint64, method string, timeout time.Duration, dispatcher inf.IRpcDispatcher, callbacks []dto.CompletionFunc, cbParams []interface{}) *CallState {
	s := newCallState()
	s.XContext = xcontext.New(ctx)
	s.reqID = reqID
	s.method = method
	s.timeout = timeout
	s.dispatcher = dispatcher
	if len(callbacks) > 0 {
		s.callbacks = callbacks
	}
	if len(cbParams) > 0 {
		s.cbParams = cbParams
	}
	return s
}

func (s *CallState) ReqID() uint64          { return s.reqID }
func (s *CallState) Method() string         { return s.method }
func (s *CallState) Timeout() time.Duration { return s.timeout }

// GetType 使 CallState 可作为 mailbox 事件直接投递。
func (s *CallState) GetType() int32 { return event.ServiceConcurrentCallback }

func (s *CallState) setTimerID(timerID uint64) { s.timerID = timerID }
func (s *CallState) timerId() uint64           { return s.timerID }

func (s *CallState) SetResult(resp interface{}, err error) {
	s.resp = resp
	s.err = err
}

func (s *CallState) Response() interface{} { return s.resp }
func (s *CallState) Error() error          { return s.err }

func (s *CallState) NeedCallback() bool { return len(s.callbacks) > 0 }

// Complete 根据是否需要回调，选择投递回调事件或唤醒同步等待。
//
// - AsyncCall：投递 event.ServiceConcurrentCallback 到 dispatcher，在回调末尾自动 Release。
// - Call：唤醒 Wait()，由调用方在读取结果后手动 Release。
func (s *CallState) Complete() {
	if s.NeedCallback() {
		s.dispatchCallbackEvent()
		return
	}
	s.signalDone()
}

func (s *CallState) signalDone() {
	select {
	case s.done <- struct{}{}:
	default:
	}
}

func (s *CallState) Wait() {
	<-s.done
}

// DoCallback 在调用方 mailbox goroutine 中执行回调。
// 该方法用于挂载到 event.ServiceConcurrentCallback。
func (s *CallState) DoCallback(ctx context.Context) {
	for _, cb := range s.callbacks {
		cb(ctx, s.resp, s.err, s.cbParams...)
	}
}

func (s *CallState) dispatchCallbackEvent() {
	if s.dispatcher == nil || s.dispatcher.IsClosed() {
		getCallStatePool().Put(s)
		return
	}
	// 直接投递 CallState，本身实现了 IEvent + IConcurrentCallback。
	if err := s.dispatcher.PostMessage(s.XContext, s); err != nil {
		getCallStatePool().Put(s)
	}
}

// Release 仅用于同步 Call 路径：调用方在 Wait 结束后手动释放。
// AsyncCall 路径由 DoCallback 自动释放。
func (s *CallState) Release() {
	getCallStatePool().Put(s)
}

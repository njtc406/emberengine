// Package msgenvelope
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/13 0013 22:21
// 最后更新:  yr  2025/7/13 0013 22:21
package msgenvelope

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
)

var metaPool = pool.NewSyncPoolWrapper(
	func() *Meta {
		return &Meta{}
	},
	func() pool.IStatsRecorder {
		if isDebug() {
			return pool.NewStatsRecorder("metaPool")
		}
		return pool.NewNoStatsRecorder()
	}(),
	pool.WithRef(func(t *Meta) {
		t.Ref()
	}),
	pool.WithUnRef(func(t *Meta) bool {
		return t.UnRef()
	}),
	pool.WithReset(func(t *Meta) {
		t.Reset()
	}),
)

func NewMeta() inf.IEnvelopeMeta {
	m := metaPool.Get()
	trackMetaBorrow(m)
	return m
}

func putMeta(m *Meta) {
	if m == nil {
		return
	}
	trackMetaReturn(m)
	metaPool.Put(m)
}

type metaBorrow struct {
	stack string
}

var metaBorrowTracker = struct {
	sync.Mutex
	borrows map[uintptr]metaBorrow
}{
	borrows: make(map[uintptr]metaBorrow),
}

var metaLeakTrackEnabledOnce sync.Once
var metaLeakTrackEnabledCached bool

func metaLeakTrackEnabled() bool {
	// Stack capture is extremely expensive (especially on Windows). Keep it strictly opt-in.
	// IMPORTANT: Avoid calling os.Getenv per message; on Windows it can be a cgocall hotspot.
	if !isDebug() {
		return false
	}
	metaLeakTrackEnabledOnce.Do(func() {
		metaLeakTrackEnabledCached = os.Getenv("META_LEAK_TRACK") == "1"
	})
	return metaLeakTrackEnabledCached
}

func trackMetaBorrow(m *Meta) {
	if m == nil || !metaLeakTrackEnabled() {
		return
	}
	ptr := uintptr(unsafe.Pointer(m))
	stack := captureStack(3)
	metaBorrowTracker.Lock()
	metaBorrowTracker.borrows[ptr] = metaBorrow{stack: stack}
	metaBorrowTracker.Unlock()
}

func trackMetaReturn(m *Meta) {
	if m == nil || !metaLeakTrackEnabled() {
		return
	}
	ptr := uintptr(unsafe.Pointer(m))
	metaBorrowTracker.Lock()
	delete(metaBorrowTracker.borrows, ptr)
	metaBorrowTracker.Unlock()
}

func DumpMetaPoolLeaks(max int) string {
	if !metaLeakTrackEnabled() {
		return ""
	}
	if max <= 0 {
		max = 10
	}
	metaBorrowTracker.Lock()
	defer metaBorrowTracker.Unlock()
	if len(metaBorrowTracker.borrows) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("metaPool leak tracker: %d Meta still borrowed\n", len(metaBorrowTracker.borrows)))
	i := 0
	for ptr, info := range metaBorrowTracker.borrows {
		i++
		b.WriteString(fmt.Sprintf("\n[%d] Meta@0x%x borrowed at:\n%s\n", i, ptr, info.stack))
		if i >= max {
			if len(metaBorrowTracker.borrows) > max {
				b.WriteString(fmt.Sprintf("\n... and %d more\n", len(metaBorrowTracker.borrows)-max))
			}
			break
		}
	}
	return b.String()
}

func captureStack(skip int) string {
	const maxFrames = 64
	pcs := make([]uintptr, maxFrames)
	n := runtime.Callers(skip, pcs)
	frames := runtime.CallersFrames(pcs[:n])

	var b strings.Builder
	for {
		frame, more := frames.Next()
		// 过滤掉本包内部的追踪函数栈，减少噪音
		if strings.Contains(frame.Function, "msgenvelope.captureStack") ||
			strings.Contains(frame.Function, "msgenvelope.trackMeta") {
			if !more {
				break
			}
			continue
		}
		b.WriteString(fmt.Sprintf("%s\n\t%s:%d\n", frame.Function, frame.File, frame.Line))
		if !more {
			break
		}
	}
	return b.String()
}

type Meta struct {
	dto.DataRef
	locker sync.RWMutex

	senderPid   *actor.PID         // 发送者
	receiverPid *actor.PID         // 接收者
	sender      inf.IRpcDispatcher // 发送者客户端(用于回复)
	reqID       uint64             // 请求ID(主要用于monitor区分不同的call)
	deadline    int64              // 超时时间(单位: 纳秒)
	callbacks   dto.CompletionFuncs
	cbParams    []interface{}
}

func (e *Meta) Reset() {
	e.senderPid = nil
	e.receiverPid = nil
	e.sender = nil
	e.reqID = 0
	e.deadline = 0
	e.callbacks = nil
	e.cbParams = nil
}

func (e *Meta) SetSenderPid(senderPid *actor.PID) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.senderPid = senderPid
}

func (e *Meta) SetReceiverPid(receiverPid *actor.PID) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.receiverPid = receiverPid
}

func (e *Meta) SetDispatcher(client inf.IRpcDispatcher) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.sender = client
}

func (e *Meta) SetReqId(reqId uint64) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.reqID = reqId
}
func (e *Meta) SetDeadline(deadline int64) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.deadline = deadline
}

func (e *Meta) SetCallbacks(callbacks dto.CompletionFuncs, cbParams []interface{}) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.callbacks = callbacks
	e.cbParams = cbParams
}

func (e *Meta) GetSenderPid() *actor.PID {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.senderPid
}

func (e *Meta) GetReceiverPid() *actor.PID {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.receiverPid
}

func (e *Meta) GetDispatcher() inf.IRpcDispatcher {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.sender
}

func (e *Meta) GetReqId() uint64 {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.reqID
}

func (e *Meta) GetDeadline() int64 {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.deadline
}

func (e *Meta) GetCallback() (callback dto.CompletionFuncs, cbParams []interface{}) {
	e.locker.Lock()
	defer e.locker.Unlock()
	return e.callbacks, e.cbParams
}

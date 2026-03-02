package monitor

import (
	"context"
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/log"

	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
)

func TestRpcMonitor_Add(t *testing.T) {
	if log.SysLogger == nil {
		l, err := log.NewLogger(&log.LoggerConf{Stdout: true, Caller: false, Color: false, Level: "debug"}, true)
		if err != nil {
			t.Fatalf("init logger failed: %v", err)
		}
		log.SysLogger = l
	}
	timingwheel.Start(time.Millisecond, 64, log.SysLogger)
	defer timingwheel.Stop()

	rm := GetRpcMonitor()
	rm.Start()
	defer rm.Stop()
	reqId := rm.GenSeq()
	state := NewCallState(context.Background(), reqId, "test", time.Second, nil, nil, nil)
	rm.Add(state)
}

func TestRpcMonitor_Remove(t *testing.T) {
	if log.SysLogger == nil {
		l, err := log.NewLogger(&log.LoggerConf{Stdout: true, Caller: false, Color: false, Level: "debug"}, true)
		if err != nil {
			t.Fatalf("init logger failed: %v", err)
		}
		log.SysLogger = l
	}
	timingwheel.Start(time.Millisecond, 64, log.SysLogger)
	defer timingwheel.Stop()

	rm := GetRpcMonitor()
	rm.Start()
	defer rm.Stop()
	const reqId = 1
	state := NewCallState(context.Background(), reqId, "test", time.Second, nil, nil, nil)
	rm.Add(state)
	st := rm.Remove(reqId)
	if st != nil {
		st.Release()
	}
	nf := rm.Get(reqId)
	if nf != nil {
		t.Error("remove failed")
	}
}

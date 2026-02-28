package monitor

import (
	"context"
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/asynclib"

	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
)

func newTestLogger(t *testing.T) log.ILoggerX {
	t.Helper()
	l, err := log.NewLogger(&log.LoggerConf{Stdout: true, Caller: false, Color: false, Level: "debug"}, true)
	if err != nil {
		t.Fatalf("init logger failed: %v", err)
	}
	return l
}

func newTestMonitor(t *testing.T) (*RpcMonitor, func()) {
	t.Helper()
	logger := newTestLogger(t)
	tw := timingwheel.NewTimingWheel(time.Millisecond, 64, logger)
	tw.Start()
	p, err := asynclib.NewPool(32)
	if err != nil {
		t.Fatalf("init pool failed: %v", err)
	}
	rm := NewRpcMonitor().Init(&config.RpcMonitorConf{MonitorTimerSize: 1000, MonitorBucketSize: 20}, logger, tw, p)
	if err = rm.Start(); err != nil {
		t.Fatalf("start monitor failed: %v", err)
	}
	cleanup := func() {
		rm.Stop()
		p.Release()
		tw.Stop()
	}
	return rm, cleanup
}

func TestRpcMonitor_Add(t *testing.T) {
	rm, cleanup := newTestMonitor(t)
	defer cleanup()
	reqId := rm.GenSeq()
	state := NewCallState(context.Background(), reqId, "test", time.Second, nil, nil, nil)
	rm.Add(state)
}

func TestRpcMonitor_Remove(t *testing.T) {
	rm, cleanup := newTestMonitor(t)
	defer cleanup()
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

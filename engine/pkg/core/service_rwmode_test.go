package core

import (
	"context"
	"errors"
	"testing"

	"github.com/njtc406/emberengine/engine/pkg/actor/mailbox"
	"github.com/njtc406/emberengine/engine/pkg/actor/mailbox/job"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/rpc/message/msgenvelope"
)

type testReadOnlyMethodMgr struct {
	readOnly map[string]bool
}

func (m *testReadOnlyMethodMgr) AddMethodFunc(name string, fn def.MethodCallFunc) {}
func (m *testReadOnlyMethodMgr) GetMethodFunc(name string) (def.MethodCallFunc, bool) {
	return nil, false
}
func (m *testReadOnlyMethodMgr) RemoveMethods(names []string) {}
func (m *testReadOnlyMethodMgr) MarkReadOnly(name string) {
	if m.readOnly == nil {
		m.readOnly = make(map[string]bool)
	}
	m.readOnly[name] = true
}
func (m *testReadOnlyMethodMgr) IsReadOnly(name string) bool {
	if m.readOnly == nil {
		return false
	}
	return m.readOnly[name]
}

func TestSetJobRWMode_MarksReadOnlyRpcAsRead(t *testing.T) {
	svc := &Service{Module: Module{methodMgr: &testReadOnlyMethodMgr{readOnly: map[string]bool{"RpcRead": true}}}}

	rpcJob := job.NewRpcJob()
	defer rpcJob.Release()
	env := msgenvelope.NewMsgEnvelope()
	defer env.Release()
	data := msgenvelope.NewData()
	data.SetMethod("RpcRead")
	env.SetData(data)
	rpcJob.SetPayload(env)

	svc.setJobRWMode(rpcJob)
	if rpcJob.GetRWMode() != def.RWModeRead {
		t.Fatalf("expected read-only rpc job to be marked RWModeRead, got=%v", rpcJob.GetRWMode())
	}
}

func TestSetJobRWMode_ReplyRpcRemainsWrite(t *testing.T) {
	svc := &Service{Module: Module{methodMgr: &testReadOnlyMethodMgr{readOnly: map[string]bool{"RpcRead": true}}}}

	rpcJob := job.NewRpcJob()
	defer rpcJob.Release()
	env := msgenvelope.NewMsgEnvelope()
	defer env.Release()
	data := msgenvelope.NewData()
	data.SetMethod("RpcRead")
	data.SetReply()
	env.SetData(data)
	rpcJob.SetPayload(env)

	svc.setJobRWMode(rpcJob)
	if rpcJob.GetRWMode() != def.RWModeWrite {
		t.Fatalf("expected reply rpc job to remain RWModeWrite, got=%v", rpcJob.GetRWMode())
	}
}

func TestSetJobRWMode_NonRpcJobRemainsWrite(t *testing.T) {
	svc := &Service{Module: Module{methodMgr: &testReadOnlyMethodMgr{readOnly: map[string]bool{"RpcRead": true}}}}

	timerJob := job.NewTimerJob()
	defer timerJob.Release()

	svc.setJobRWMode(timerJob)
	if timerJob.GetRWMode() != def.RWModeWrite {
		t.Fatalf("expected non-rpc job to remain RWModeWrite, got=%v", timerJob.GetRWMode())
	}
}

func TestSetJobRWMode_UnknownMethodRemainsWrite(t *testing.T) {
	svc := &Service{Module: Module{methodMgr: &testReadOnlyMethodMgr{readOnly: map[string]bool{"RpcRead": true}}}}

	rpcJob := job.NewRpcJob()
	defer rpcJob.Release()
	env := msgenvelope.NewMsgEnvelope()
	defer env.Release()
	data := msgenvelope.NewData()
	data.SetMethod("RpcWrite")
	env.SetData(data)
	rpcJob.SetPayload(env)

	svc.setJobRWMode(rpcJob)
	if rpcJob.GetRWMode() != def.RWModeWrite {
		t.Fatalf("expected unknown method rpc job to remain RWModeWrite, got=%v", rpcJob.GetRWMode())
	}
}

func newRWMailboxService(t *testing.T, serviceName string, enableRW bool) *Service {
	t.Helper()
	baseLogger, err := log.NewDefaultLogger(nil)
	if err != nil {
		t.Fatalf("new default logger failed: %v", err)
	}
	t.Cleanup(func() {
		type closeable interface{ Close() error }
		if c, ok := interface{}(baseLogger).(closeable); ok {
			_ = c.Close()
		}
	})

	svc := &Service{name: serviceName}
	svc.Module.logger = baseLogger
	svc.Module.ILoggerX = baseLogger
	mbConf := &config.MailboxConf{
		EnableRWMode: enableRW,
		SchedulePolicy: &config.WorkerSchedulePolicy{
			InitialWorkerNum: 2,
		},
	}
	svc.mailbox, _ = mailbox.NewMailbox(mbConf, baseLogger, svc, nil)
	return svc
}

func TestPostJob_ReadOnlySelfPostingRejected(t *testing.T) {
	svc := newRWMailboxService(t, "svc-a", true)

	j := job.NewTimerJob()
	defer j.Release()
	ctx := context.WithValue(context.Background(), def.RWContextKey, def.RWContextInfo{
		Mode:          def.RWModeRead,
		SourceService: "svc-a",
	})
	j.SetContext(ctx)

	err := svc.PostJob(j)
	if !errors.Is(err, def.ErrReadOnlyPostJob) {
		t.Fatalf("expected ErrReadOnlyPostJob, got=%v", err)
	}
}

func TestPostJob_ReadOnlyCrossServiceNotRejected(t *testing.T) {
	svc := newRWMailboxService(t, "svc-a", true)

	j := job.NewTimerJob()
	defer j.Release()
	ctx := context.WithValue(context.Background(), def.RWContextKey, def.RWContextInfo{
		Mode:          def.RWModeRead,
		SourceService: "svc-b",
	})
	j.SetContext(ctx)

	err := svc.PostJob(j)
	if errors.Is(err, def.ErrReadOnlyPostJob) {
		t.Fatalf("expected cross-service posting not to be rejected by self-post guard")
	}
	if !errors.Is(err, def.ErrMailboxWorkerNotFound) {
		t.Fatalf("expected mailbox dispatch path error when workers not started, got=%v", err)
	}
}

func TestPostJob_RWDisabledIgnoresReadOnlyGuard(t *testing.T) {
	svc := newRWMailboxService(t, "svc-a", false)

	j := job.NewTimerJob()
	defer j.Release()
	ctx := context.WithValue(context.Background(), def.RWContextKey, def.RWContextInfo{
		Mode:          def.RWModeRead,
		SourceService: "svc-a",
	})
	j.SetContext(ctx)

	err := svc.PostJob(j)
	if errors.Is(err, def.ErrReadOnlyPostJob) {
		t.Fatalf("expected read-only guard disabled when mailbox RW mode is off")
	}
	if !errors.Is(err, def.ErrMailboxWorkerNotFound) {
		t.Fatalf("expected mailbox dispatch path error when workers not started, got=%v", err)
	}
}

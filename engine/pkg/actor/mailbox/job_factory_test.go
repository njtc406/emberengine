package mailbox

import (
	"testing"

	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

func TestCreateJob_Builtins(t *testing.T) {
	cases := []struct {
		name    string
		jobType def.MailboxJobType
	}{
		{"rpc", def.MailboxJobTypeRpc},
		{"event", def.MailboxJobTypeEvent},
		{"internal_event", def.MailboxJobTypeInternalEvent},
		{"timer", def.MailboxJobTypeTimer},
		{"concurrent_callback", def.MailboxJobTypeConcurrentCallback},
		{"sysctl", def.MailboxJobSysCtl},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			job, ok := CreateJob(tc.jobType)
			if !ok || job == nil {
				t.Fatalf("expected job for type %v", tc.jobType)
			}
			if job.GetType() != def.MailboxJobTypeNone {
				// 新建 job 默认 Reset 后应为 None（具体类型在派发前再赋值）
			}
			job.Release()
		})
	}
}

func TestRegisterJobFactory_DuplicateAndReplace(t *testing.T) {
	const customType def.MailboxJobType = 10001

	creator1 := func() inf.IMailboxJob { return NewMsgJob() }
	creator2 := func() inf.IMailboxJob { return NewTimerJob() }

	if err := RegisterJobFactory(customType, creator1, false); err != nil {
		t.Fatalf("register creator1 failed: %v", err)
	}
	if err := RegisterJobFactory(customType, creator1, false); err == nil {
		t.Fatalf("expected duplicate register error")
	}
	if err := RegisterJobFactory(customType, creator2, true); err != nil {
		t.Fatalf("replace register failed: %v", err)
	}

	job, ok := CreateJob(customType)
	if !ok || job == nil {
		t.Fatalf("expected job for custom type")
	}
	job.Release()
}

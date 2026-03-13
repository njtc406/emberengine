package job

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
		{"timer", def.MailboxJobTypeTimer},
		{"concurrent_callback", def.MailboxJobTypeConcurrentCallback},
		{"sysctl", def.MailboxJobTypeSysCtl},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			job, ok := CreateJob(tc.jobType)
			if !ok || job == nil {
				t.Fatalf("expected job for type %v", tc.jobType)
			}
			if job.GetType() != tc.jobType {
				t.Fatalf("expected type %v, got %v", tc.jobType, job.GetType())
			}
			job.Release()
		})
	}
}

type TestJob struct {
	Job[int]
}

func (j *TestJob) Release() {
}

type Test1Job struct {
	Job[string]
}

func (j *Test1Job) Release() {
}

func TestRegisterJobFactory_DuplicateAndReplace(t *testing.T) {
	resetFactoryFrozenForTest()
	const customType def.MailboxJobType = 10001
	const customType1 def.MailboxJobType = 10002

	creator1 := func() inf.IMailboxJob { return &TestJob{} }
	creator2 := func() inf.IMailboxJob { return &Test1Job{} }
	getter1 := func(j inf.IMailboxJob) any { return j.(*TestJob).GetPayload() }
	getter2 := func(j inf.IMailboxJob) any { return j.(*Test1Job).GetPayload() }

	if err := RegisterJobFactory(customType, creator1, getter1); err != nil {
		t.Fatalf("register creator1 failed: %v", err)
	}
	if err := RegisterJobFactory(customType1, creator2, getter2); err != nil {
		t.Fatalf("register creator2 failed: %v", err)
	}

	job, ok := CreateJob(customType)
	if !ok || job == nil {
		t.Fatalf("expected job for custom type")
	}
	job.Release()

	job1, ok := CreateJob(customType1)
	if !ok || job1 == nil {
		t.Fatalf("expected job for custom type1")
	}
	job1.Release()
}

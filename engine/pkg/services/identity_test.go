package services

import (
	"testing"

	"github.com/njtc406/emberengine/v2/engine/pkg/config"
	inf "github.com/njtc406/emberengine/v2/engine/pkg/interfaces"
)

func TestServiceManagerPartitionNameUniqueness(t *testing.T) {
	for _, tc := range []struct {
		name       string
		secondName string
		partition  string
		duplicate  bool
	}{
		{"duplicate", "Gate", "1", true},
		{"different partition", "Gate", "2", false},
		{"different name", "Chat", "1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sm := newTestServiceManager(t)
			built := 0
			for _, class := range []string{"identity-a", "identity-b"} {
				registerTestService(t, class, func() inf.IService { built++; return &testManagedService{} })
			}
			err := sm.Init(&config.ServiceConf{StartServices: []*config.ServiceInitConf{
				{ClassName: "identity-a", ServiceName: "Gate", Partition: "1"},
				{ClassName: "identity-b", ServiceName: tc.secondName, Partition: tc.partition},
			}})
			if (err != nil) != tc.duplicate {
				t.Fatalf("Init error = %v, duplicate = %v", err, tc.duplicate)
			}
			if tc.duplicate && built != 0 {
				t.Fatalf("duplicate configuration initialized %d services before rejection", built)
			}
		})
	}
}

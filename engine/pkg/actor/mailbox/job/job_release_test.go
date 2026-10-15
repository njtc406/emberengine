package job

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/njtc406/emberengine/engine/pkg/def"
)

// ============================================================================
// P0-1.2: Job.Release 释放 payload 的单元测试
//
// 验证 Job 对象池的所有权契约：
// 1. Get → Release 后 DataRef 变为 unref
// 2. 重复 Release 不 panic（CAS 幂等）
// 3. 所有内置 Job 类型 Get/Release 正确
// ============================================================================

func TestRpcJob_Release_UnrefsDataRef(t *testing.T) {
	j := NewRpcJob()
	if !j.IsRef() {
		t.Fatal("new RpcJob should be ref'd after pool.Get")
	}
	j.Release()
	if j.IsRef() {
		t.Fatal("RpcJob should be unref'd after Release")
	}
}

func TestRpcJob_DoubleRelease_NoPanic(t *testing.T) {
	j := NewRpcJob()
	j.Release()
	// 第二次 Release 不应 panic（CAS 失败会被静默忽略）
	j.Release()
}

func TestEventBusJob_Release_UnrefsDataRef(t *testing.T) {
	j := NewEventBusJob()
	if !j.IsRef() {
		t.Fatal("new EventBusJob should be ref'd")
	}
	j.Release()
	if j.IsRef() {
		t.Fatal("EventBusJob should be unref'd after Release")
	}
}

func TestTimerJob_Release_UnrefsDataRef(t *testing.T) {
	j := NewTimerJob()
	if !j.IsRef() {
		t.Fatal("new TimerJob should be ref'd")
	}
	j.Release()
	if j.IsRef() {
		t.Fatal("TimerJob should be unref'd after Release")
	}
}

func TestConcurrentCallbackJob_Release_UnrefsDataRef(t *testing.T) {
	j := NewConcurrentCallbackJob()
	if !j.IsRef() {
		t.Fatal("new ConcurrentCallbackJob should be ref'd")
	}
	j.Release()
	if j.IsRef() {
		t.Fatal("ConcurrentCallbackJob should be unref'd after Release")
	}
}

func TestSysCtlJob_Release_UnrefsDataRef(t *testing.T) {
	j := NewSysCtlJob()
	if !j.IsRef() {
		t.Fatal("new SysCtlJob should be ref'd")
	}
	j.Release()
	if j.IsRef() {
		t.Fatal("SysCtlJob should be unref'd after Release")
	}
}

func TestAllBuiltinJobs_GetReleaseCycle(t *testing.T) {
	types := []struct {
		name    string
		jobType def.MailboxJobType
	}{
		{"rpc", def.MailboxJobTypeRpc},
		{"event", def.MailboxJobTypeEvent},
		{"timer", def.MailboxJobTypeTimer},
		{"concurrent_callback", def.MailboxJobTypeConcurrentCallback},
		{"sysctl", def.MailboxJobTypeSysCtl},
	}

	for _, tc := range types {
		t.Run(tc.name, func(t *testing.T) {
			j, ok := CreateJob(tc.jobType)
			if !ok || j == nil {
				t.Fatalf("CreateJob(%v) should succeed", tc.jobType)
			}
			if j.GetType() != tc.jobType {
				t.Fatalf("type = %v, want %v", j.GetType(), tc.jobType)
			}
			j.Release()
		})
	}
}

func TestRpcJob_ConcurrentRelease_NoRace(t *testing.T) {
	const N = 100
	var wg sync.WaitGroup
	var doubleRelease atomic.Int64

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j := NewRpcJob()
			// 两个 goroutine 竞争 Release 同一个 Job
			var inner sync.WaitGroup
			inner.Add(2)
			go func() {
				defer inner.Done()
				j.Release()
			}()
			go func() {
				defer inner.Done()
				// 第二次 Release 不应 panic，CAS 失败即静默
				if !j.UnRef() {
					doubleRelease.Add(1)
				}
			}()
			inner.Wait()
		}()
	}
	wg.Wait()
	// 至少有一部分应该检测到重复释放
	t.Logf("double release CAS failures: %d/%d", doubleRelease.Load(), N)
}

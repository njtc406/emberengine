package actor

import (
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
)

// TestPID_IsMasterNode_AfterUnmarshalWithoutSync 验证：当反序列化入口只写入
// proto 字段 IsMaster 而未调用 SyncMasterFlag 时，IsMasterNode() 会返回 false。
// 典型场景：历史数据 / protojson 只含 "IsMaster":true 且缺省 MasterFlag。
// 这是一个“提示性”测试：反序列化入口必须显式调用 SyncMasterFlag。
func TestPID_IsMasterNode_AfterUnmarshalWithoutSync(t *testing.T) {
	// 模拟旧数据：只有 IsMaster，没有 MasterFlag
	raw := []byte(`{"IsMaster":true}`)
	dst := &PID{}
	if err := protojson.Unmarshal(raw, dst); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !dst.IsMaster {
		t.Fatalf("proto IsMaster bool should be true after unmarshal")
	}
	if dst.MasterFlag != 0 {
		t.Fatalf("expected MasterFlag=0 from legacy payload, got %d", dst.MasterFlag)
	}
	// 未调用 SyncMasterFlag：原子字段仍为 0，IsMasterNode 返回 false
	if dst.IsMasterNode() {
		t.Fatalf("IsMasterNode should be false before SyncMasterFlag; missing call at unmarshal entrypoint")
	}

	dst.SyncMasterFlag()
	if !dst.IsMasterNode() {
		t.Fatalf("IsMasterNode should be true after SyncMasterFlag")
	}
}

func TestPID_SetMaster_Concurrent(t *testing.T) {
	p := &PID{}
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			p.SetMaster(i%2 == 0)
		}
		close(done)
	}()
	for i := 0; i < 1000; i++ {
		_ = p.IsMasterNode()
	}
	<-done
}

// TestPID_SetMaster_DoesNotTouchIsMasterBool 验证 SetMaster 不应写
// IsMaster bool 字段（避免与序列化路径形成 data race）。
// IsMaster 只能由 PrepareForMarshal 在序列化出口投影。
func TestPID_SetMaster_DoesNotTouchIsMasterBool(t *testing.T) {
	p := &PID{}
	p.SetMaster(true)
	if p.IsMaster {
		t.Fatalf("SetMaster(true) must not write IsMaster bool; expected false (untouched), got true")
	}
	if !p.IsMasterNode() {
		t.Fatalf("SetMaster(true) should set MasterFlag so IsMasterNode()==true")
	}
	// 调用 PrepareForMarshal 后 IsMaster 才反映 MasterFlag
	p.PrepareForMarshal()
	if !p.IsMaster {
		t.Fatalf("PrepareForMarshal should project MasterFlag to IsMaster; got false")
	}

	p.SetMaster(false)
	if !p.IsMaster {
		t.Fatalf("SetMaster(false) must not touch IsMaster bool; expected previous value true")
	}
	p.PrepareForMarshal()
	if p.IsMaster {
		t.Fatalf("PrepareForMarshal should project false; got true")
	}
}

// TestPID_PrepareForMarshal_ConcurrentWithSetMaster 验证：序列化出口的
// PrepareForMarshal 与运行时并发 SetMaster 不构成 data race（因为 SetMaster
// 只写 MasterFlag，PrepareForMarshal 只写 IsMaster；两者无重叠字段）。
func TestPID_PrepareForMarshal_ConcurrentWithSetMaster(t *testing.T) {
	p := &PID{}
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			p.SetMaster(i%2 == 0)
		}
		close(done)
	}()
	for i := 0; i < 1000; i++ {
		p.PrepareForMarshal()
	}
	<-done
}

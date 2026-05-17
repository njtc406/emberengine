package msgenvelope

import (
	"testing"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/def"
)

// BenchmarkMsgEnvelope_SetGetMeta 测量 SetMeta/GetMeta 的锁开销。
func BenchmarkMsgEnvelope_SetGetMeta(b *testing.B) {
	envelope := NewMsgEnvelope()
	defer envelope.Release()
	meta := NewMeta()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		envelope.SetMeta(meta)
		_ = envelope.GetMeta()
	}
}

// BenchmarkMsgEnvelope_SetGetData 测量 SetData/GetData 的锁开销。
func BenchmarkMsgEnvelope_SetGetData(b *testing.B) {
	envelope := NewMsgEnvelope()
	defer envelope.Release()
	data := NewData()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		envelope.SetData(data)
		_ = envelope.GetData()
	}
}

// BenchmarkMsgEnvelope_GetPriority 测量高频只读路径的锁开销。
func BenchmarkMsgEnvelope_GetPriority(b *testing.B) {
	envelope := NewMsgEnvelope()
	defer envelope.Release()
	envelope.SetPriority(def.PriorityNormal)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = envelope.GetPriority()
	}
}

// BenchmarkMsgEnvelope_ConcurrentGet 多 goroutine 并发读。
func BenchmarkMsgEnvelope_ConcurrentGet(b *testing.B) {
	envelope := NewMsgEnvelope()
	defer envelope.Release()
	meta := NewMeta()
	data := NewData()
	envelope.SetMeta(meta)
	envelope.SetData(data)
	envelope.SetPriority(def.PriorityNormal)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = envelope.GetMeta()
			_ = envelope.GetData()
			_ = envelope.GetPriority()
		}
	})
}

// BenchmarkPID_SnapshotForWire 测量 proto.Clone + PrepareForMarshal 开销。
func BenchmarkPID_SnapshotForWire(b *testing.B) {
	pid := &actor.PID{
		Address:     "localhost:6610",
		Name:        "TestService",
		ServiceType: "game",
		ServiceId:   "svc-001",
		Partition:   1,
		Version:     1,
		RpcType:     "nats",
		NodeUid:     "node-abc-123",
		ServiceUid:  "svc-uid-456",
	}
	pid.SetMaster(true)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = actor.SnapshotForWire(pid)
	}
}

// BenchmarkPID_MarshalPID 测量完整的 Clone+Prepare+Marshal 序列化路径。
func BenchmarkPID_MarshalPID(b *testing.B) {
	pid := &actor.PID{
		Address:     "localhost:6610",
		Name:        "TestService",
		ServiceType: "game",
		ServiceId:   "svc-001",
		Partition:   1,
		Version:     1,
		RpcType:     "nats",
		NodeUid:     "node-abc-123",
		ServiceUid:  "svc-uid-456",
	}
	pid.SetMaster(true)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = actor.MarshalPID(pid)
	}
}

// BenchmarkPID_SnapshotForWire_Parallel 多 goroutine 并发 snapshot。
func BenchmarkPID_SnapshotForWire_Parallel(b *testing.B) {
	pid := &actor.PID{
		Address:     "localhost:6610",
		Name:        "TestService",
		ServiceType: "game",
		ServiceId:   "svc-001",
		Partition:   1,
		Version:     1,
		RpcType:     "nats",
		NodeUid:     "node-abc-123",
		ServiceUid:  "svc-uid-456",
	}
	pid.SetMaster(true)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = actor.SnapshotForWire(pid)
		}
	})
}

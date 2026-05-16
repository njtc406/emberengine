package profiler

import (
	"testing"
	"time"
)

// TestPushRecordLog_EvictsFromRecordNotStack 验证 B1 修复：
// pushRecordLog 满时应从 record 列表淘汰最旧记录，而不是从 stack 列表删除。
func TestPushRecordLog_EvictsFromRecordNotStack(t *testing.T) {
	p := NewProfiler(nil)
	p.SetMaxRecordNum(3)

	// 模拟一个正在进行中的 Push（stack 中有元素）
	p.stack.PushBack(&Element{tagName: "active-tag", pushTime: time.Now()})

	// 填满 record 列表
	for i := 0; i < 3; i++ {
		p.pushRecordLog(&Record{RType: OvertimeType, CostTime: time.Millisecond, RecordName: "old"})
	}

	// stack 不应该被影响
	if p.stack.Len() != 1 {
		t.Fatalf("expected stack.Len()=1, got %d (stack was incorrectly modified)", p.stack.Len())
	}

	// 再插入一条，触发淘汰
	p.pushRecordLog(&Record{RType: OvertimeType, CostTime: time.Millisecond, RecordName: "new"})

	// record 应该保持最大 3 条
	if p.record.Len() != 3 {
		t.Fatalf("expected record.Len()=3, got %d", p.record.Len())
	}

	// stack 仍然不受影响
	if p.stack.Len() != 1 {
		t.Fatalf("expected stack.Len()=1 after eviction, got %d", p.stack.Len())
	}

	// 最前面的 record 应该是第二条旧记录（第一条被淘汰）
	front := p.record.Front().Value.(*Record)
	if front.RecordName != "old" {
		t.Fatalf("expected front record to be 'old', got %q", front.RecordName)
	}

	// 最后一条是新记录
	back := p.record.Back().Value.(*Record)
	if back.RecordName != "new" {
		t.Fatalf("expected back record to be 'new', got %q", back.RecordName)
	}
}

// TestPushPop_NoStackCorruption 验证 Push+Pop 正常工作，record 不会破坏 stack
func TestPushPop_NoStackCorruption(t *testing.T) {
	p := NewProfiler(nil)
	p.SetMaxRecordNum(2)
	p.SetOverTime(0) // 任何耗时都记录

	// Push 多个标签
	a1 := p.Push("tag-1")
	a2 := p.Push("tag-2")
	a3 := p.Push("tag-3")

	// stack 应该有 3 个元素
	if p.stack.Len() != 3 {
		t.Fatalf("expected stack.Len()=3, got %d", p.stack.Len())
	}

	// Pop 顺序无关紧要，验证不 panic
	time.Sleep(time.Millisecond) // 确保超过 overTime
	a2.Pop()
	a3.Pop()
	a1.Pop()

	// stack 应该清空
	if p.stack.Len() != 0 {
		t.Fatalf("expected stack.Len()=0 after all pops, got %d", p.stack.Len())
	}

	// record 应不超过 maxRecordNum
	if p.record.Len() > 2 {
		t.Fatalf("expected record.Len()<=2, got %d", p.record.Len())
	}
}

// TestMaxRecordNum_UsesInstanceField 验证使用实例字段而非全局常量
func TestMaxRecordNum_UsesInstanceField(t *testing.T) {
	p := NewProfiler(nil)
	p.SetMaxRecordNum(5)

	// 插入 10 条记录
	for i := 0; i < 10; i++ {
		p.pushRecordLog(&Record{RType: OvertimeType, CostTime: time.Millisecond, RecordName: "rec"})
	}

	// 应该只保留 5 条
	if p.record.Len() != 5 {
		t.Fatalf("expected record.Len()=5 (maxRecordNum), got %d", p.record.Len())
	}
}

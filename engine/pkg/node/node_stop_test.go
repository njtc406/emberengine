package node

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/stretchr/testify/assert"
)

// ============================================================================
// P1-4.2: Node Stop 幂等和停止顺序测试
// ============================================================================

// newMinimalStoppableNode 构造一个可以安全调用 Stop() 的最小 Node。
// 不启动任何真实组件，只设置 Config + Logger 以避免 nil panic。
func newMinimalStoppableNode(t *testing.T) *Node {
	t.Helper()
	base, err := log.NewLogger(&log.LoggerConf{Stdout: false, Caller: false, Color: false, Level: "error"}, true)
	if err != nil {
		t.Fatalf("new logger failed: %v", err)
	}
	n := &Node{
		Config: &config.Config{
			NodeConf: &config.NodeConf{
				PVPath:   t.TempDir(),
				NodeId:   "test-node",
				NodeType: "test",
			},
		},
		Logger: base,
	}
	return n
}

func TestNodeStop_Idempotent(t *testing.T) {
	n := newMinimalStoppableNode(t)

	n.Stop()
	n.Stop() // 第二次调用不应 panic

	if !n.stopped.Load() {
		t.Fatal("node should be marked as stopped")
	}
}

func TestNodeStop_ConcurrentSafe(t *testing.T) {
	n := newMinimalStoppableNode(t)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n.Stop()
		}()
	}
	wg.Wait()

	if !n.stopped.Load() {
		t.Fatal("node should be marked as stopped after concurrent stops")
	}
}

func TestNodeStop_CleanupsExecuteInReverseOrder(t *testing.T) {
	n := newMinimalStoppableNode(t)

	var order []int
	n.stopCleanups = []nodeCleanup{
		{name: "first", fn: func() { order = append(order, 1) }, includeInStop: true},
		{name: "second", fn: func() { order = append(order, 2) }, includeInStop: true},
		{name: "third", fn: func() { order = append(order, 3) }, includeInStop: true},
	}

	n.Stop()

	if len(order) != 3 {
		t.Fatalf("expected 3 cleanups executed, got %d", len(order))
	}
	if order[0] != 3 || order[1] != 2 || order[2] != 1 {
		t.Errorf("expected reverse order [3,2,1], got %v", order)
	}
}

func TestNodeStop_CleanupPanicDoesNotBlockOthers(t *testing.T) {
	n := newMinimalStoppableNode(t)

	var secondCalled bool
	n.stopCleanups = []nodeCleanup{
		{name: "ok-cleanup", fn: func() { secondCalled = true }, includeInStop: true},
		{name: "panic-cleanup", fn: func() { panic("cleanup panic") }, includeInStop: true},
	}

	n.Stop()

	// per-step recover 保护：panic-cleanup 不影响 ok-cleanup 执行
	assert.True(t, secondCalled, "second cleanup should execute even if earlier one panics")
}

func TestNodeStop_EmptyCleanups(t *testing.T) {
	n := newMinimalStoppableNode(t)
	n.stopCleanups = nil

	n.Stop() // 空 cleanup 列表不应 panic
}

// --- stopped 标志测试 ---

func TestNodeStopped_InitiallyFalse(t *testing.T) {
	n := &Node{}
	if n.stopped.Load() {
		t.Fatal("stopped should be false initially")
	}
}

func TestNodeStopped_AtomicProtection(t *testing.T) {
	var stopped atomic.Bool
	// CAS 语义验证
	if !stopped.CompareAndSwap(false, true) {
		t.Fatal("first CAS should succeed")
	}
	if stopped.CompareAndSwap(false, true) {
		t.Fatal("second CAS should fail")
	}
}

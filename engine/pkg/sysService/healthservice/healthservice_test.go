package healthservice

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
)

// --- mock INodeContext ---

type mockNodeContext struct {
	ready       bool
	metricsText string
}

func (m *mockNodeContext) GetConfig() inf.INodeConfig                     { return nil }
func (m *mockNodeContext) GetLogger() log.ILoggerX                        { return nil }
func (m *mockNodeContext) GetAntsPool() inf.INodePool                     { return nil }
func (m *mockNodeContext) GetTimingWheel() inf.INodeTimingWheel           { return nil }
func (m *mockNodeContext) GetDeDuplicator() inf.IDeDuplicator             { return nil }
func (m *mockNodeContext) GetNodeId() string                              { return "1" }
func (m *mockNodeContext) GetNodeType() string                            { return "test" }
func (m *mockNodeContext) GetNodeUid() string                             { return "test_1" }
func (m *mockNodeContext) IsClusterMode() bool                            { return false }
func (m *mockNodeContext) GetEndpointManager() inf.INodeEndpointManager   { return nil }
func (m *mockNodeContext) GetEventBus() inf.INodeEventBus                 { return nil }
func (m *mockNodeContext) GetRouter() inf.INodeRouter                     { return nil }
func (m *mockNodeContext) GetProfilerRegistry() inf.INodeProfilerRegistry { return nil }
func (m *mockNodeContext) GetMethodIndex() inf.INodeMethodIndex           { return nil }
func (m *mockNodeContext) IsReady() bool                                  { return m.ready }
func (m *mockNodeContext) GetRuntimeMetricsText() string                  { return m.metricsText }

// mock INodeRouter.Select to satisfy interface
type mockRouter struct{}

func (mr *mockRouter) Select(_ *actor.PID, _ ...inf.SelectParamBuilder) inf.IBus { return nil }
func (mr *mockRouter) SelectByPid(_, _ *actor.PID) inf.IBus                      { return nil }
func (mr *mockRouter) RouteByPid(_, _ *actor.PID) inf.IBus                       { return nil }
func (mr *mockRouter) SelectByRule(_ *actor.PID, _ func(*actor.PID) bool) inf.IBus {
	return nil
}
func (mr *mockRouter) SelectByServiceUid(_ *actor.PID, _ string) inf.IBus { return nil }

var _ inf.INodeContext = (*mockNodeContext)(nil)

// --- Helper: create a HealthService with mock context for testing ---

func newTestService(ctx inf.INodeContext) *HealthService {
	hs := &HealthService{}
	hs.SetNodeContext(ctx)
	return hs
}

// --- /health tests ---

func TestHandleHealth_ReturnsOK(t *testing.T) {
	hs := newTestService(nil)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	hs.handleHealth(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Errorf("body = %q, want %q", string(body), "ok")
	}
}

func TestHandleHealth_ContentType(t *testing.T) {
	hs := newTestService(nil)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	hs.handleHealth(rec, req)

	ct := rec.Result().Header.Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
}

// --- /ready tests ---

func TestHandleReady_NoContext(t *testing.T) {
	hs := newTestService(nil)
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()

	hs.handleReady(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "not ready" {
		t.Errorf("body = %q, want %q", string(body), "not ready")
	}
}

func TestHandleReady_NotReady(t *testing.T) {
	hs := newTestService(&mockNodeContext{ready: false})
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()

	hs.handleReady(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestHandleReady_Ready(t *testing.T) {
	hs := newTestService(&mockNodeContext{ready: true})
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()

	hs.handleReady(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ready" {
		t.Errorf("body = %q, want %q", string(body), "ready")
	}
}

// --- /metrics tests ---

func TestHandleMetrics_NoContext(t *testing.T) {
	hs := newTestService(nil)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	hs.handleMetrics(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestHandleMetrics_WithMetrics(t *testing.T) {
	metricsText := `# HELP ember_node_uptime_seconds Node uptime in seconds
# TYPE ember_node_uptime_seconds gauge
ember_node_uptime_seconds{node="test_1"} 300
`
	hs := newTestService(&mockNodeContext{ready: true, metricsText: metricsText})
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	hs.handleMetrics(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/plain") || !strings.Contains(ct, "version=0.0.4") {
		t.Errorf("Content-Type = %q, want Prometheus format", ct)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "ember_node_uptime_seconds") {
		t.Errorf("body should contain metrics, got:\n%s", string(body))
	}
}

func TestHandleMetrics_EmptyMetrics(t *testing.T) {
	hs := newTestService(&mockNodeContext{ready: true, metricsText: ""})
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	hs.handleMetrics(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d even with empty metrics", rec.Code, http.StatusOK)
	}
}

// --- OnRelease 幂等测试 ---

func TestOnRelease_Idempotent(t *testing.T) {
	hs := &HealthService{}
	// 调用多次不应 panic
	hs.OnRelease()
	hs.OnRelease()
	hs.OnRelease()
}

func TestOnRelease_WithServer(t *testing.T) {
	hs := &HealthService{}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	hs.server = &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: mux,
	}

	// Start server
	go func() {
		_ = hs.server.ListenAndServe()
	}()

	// Give it a moment to start
	// OnRelease should shutdown cleanly
	hs.OnRelease()
	// Second call should be no-op (sync.Once)
	hs.OnRelease()
}

// --- 并发测试 ---

func TestHandleMetrics_Concurrent(t *testing.T) {
	metricsText := "# HELP test Test\n# TYPE test gauge\ntest 42\n"
	hs := newTestService(&mockNodeContext{ready: true, metricsText: metricsText})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			rec := httptest.NewRecorder()
			hs.handleMetrics(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("concurrent metrics: status = %d", rec.Code)
			}
		}()
	}
	wg.Wait()
}

func TestHandleReady_Concurrent(t *testing.T) {
	hs := newTestService(&mockNodeContext{ready: true})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/ready", nil)
			rec := httptest.NewRecorder()
			hs.handleReady(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("concurrent ready: status = %d", rec.Code)
			}
		}()
	}
	wg.Wait()
}

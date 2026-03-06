package services

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/cluster"
	"github.com/njtc406/emberengine/engine/pkg/cluster/endpoints"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/core"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/profiler"
	"github.com/njtc406/emberengine/engine/pkg/router"
)

type testManagedService struct {
	core.Service
	initErr  error
	startErr error

	initCalled  bool
	startCalled bool
	stopCount   int
	seenCfg     interface{}

	runtimeDepsCalled bool
	nodeCtxCalled     bool
}

func (s *testManagedService) Init(src interface{}, serviceInitConf *config.ServiceInitConf, cfg interface{}) error {
	s.initCalled = true
	s.seenCfg = cfg
	return s.initErr
}

func (s *testManagedService) Start() error {
	s.startCalled = true
	return s.startErr
}

func (s *testManagedService) Stop() {
	s.stopCount++
}

func (s *testManagedService) SetRuntimeDeps(c *cluster.Cluster, em *endpoints.EndpointManager, pr *profiler.Registry, rt *router.Router) {
	s.runtimeDepsCalled = true
}

func (s *testManagedService) SetNodeContext(ctx inf.INodeContext) {
	s.nodeCtxCalled = true
}

func newTestServiceManager(t *testing.T) *ServiceManager {
	t.Helper()
	base, err := log.NewLogger(&log.LoggerConf{Stdout: false, Caller: false, Color: false, Level: "error"}, true)
	if err != nil {
		t.Fatalf("new logger failed: %v", err)
	}
	t.Cleanup(func() { _ = base.Close() })
	return NewServiceManager(log.NewLoggerX(base, log.Fields{"pkg": "services_test"}))
}

func registerTestService(t *testing.T, className string, builder func() inf.IService) {
	t.Helper()
	SetService(className, builder)
	t.Cleanup(func() {
		lock.Lock()
		delete(serviceMap, className)
		lock.Unlock()
	})
}

func TestServiceManagerInitMissingRegistration(t *testing.T) {
	sm := newTestServiceManager(t)
	conf := &config.ServiceConf{
		StartServices: []*config.ServiceInitConf{{ClassName: "svc-missing", Type: "logic", Partition: 1}},
	}

	err := sm.Init(conf)
	if err == nil {
		t.Fatalf("expected init error for unregistered service")
	}
}

func TestServiceManagerInitStartRollback(t *testing.T) {
	sm := newTestServiceManager(t)
	class1 := fmt.Sprintf("svc-class-%d", time.Now().UnixNano())
	class2 := class1 + "-2"

	var s1, s2 *testManagedService
	registerTestService(t, class1, func() inf.IService {
		s1 = &testManagedService{}
		return s1
	})
	registerTestService(t, class2, func() inf.IService {
		s2 = &testManagedService{startErr: errors.New("start failed")}
		return s2
	})

	conf := &config.ServiceConf{
		StartServices: []*config.ServiceInitConf{
			{ClassName: class1, ServiceName: "svc-1", Type: "logic", Partition: 1},
			{ClassName: class2, ServiceName: "svc-2", Type: "logic", Partition: 1},
		},
		ServicesConfMap: map[string]*config.ServiceConfig{
			"svc-1": {Cfg: "cfg-1"},
			"svc-2": {Cfg: "cfg-2"},
		},
	}

	if err := sm.Init(conf); err != nil {
		t.Fatalf("init should succeed, err=%v", err)
	}
	if len(sm.runServices) != 2 {
		t.Fatalf("expected 2 initialized services, got %d", len(sm.runServices))
	}

	if s1 == nil || s2 == nil {
		t.Fatalf("expected service instances to be created")
	}
	if !s1.initCalled || !s2.initCalled {
		t.Fatalf("expected init called on all services")
	}
	if s1.GetName() != "svc-1" || s2.GetName() != "svc-2" {
		t.Fatalf("service name override failed, got %s and %s", s1.GetName(), s2.GetName())
	}
	if s1.seenCfg != "cfg-1" || s2.seenCfg != "cfg-2" {
		t.Fatalf("service cfg binding failed, got %#v and %#v", s1.seenCfg, s2.seenCfg)
	}
	if !s1.runtimeDepsCalled || !s2.runtimeDepsCalled {
		t.Fatalf("expected runtime deps setter to be called")
	}
	if !s1.nodeCtxCalled || !s2.nodeCtxCalled {
		t.Fatalf("expected node context setter to be called")
	}

	err := sm.Start()
	if err == nil {
		t.Fatalf("expected start error from second service")
	}
	if !s1.startCalled || !s2.startCalled {
		t.Fatalf("expected start called on both services")
	}
	if s1.stopCount != 1 {
		t.Fatalf("expected rollback stop once on first service, got %d", s1.stopCount)
	}
	if s2.stopCount != 0 {
		t.Fatalf("failed service should not be rollback-stopped by manager loop, got %d", s2.stopCount)
	}
}

func TestServiceManagerStopAllReverseOrder(t *testing.T) {
	sm := newTestServiceManager(t)
	first := &testManagedService{}
	second := &testManagedService{}
	sm.runServices = []inf.IService{first, second}

	sm.StopAll()

	if second.stopCount != 1 || first.stopCount != 1 {
		t.Fatalf("expected stop on both services, got first=%d second=%d", first.stopCount, second.stopCount)
	}
}

func TestServiceManagerGetRuntimeSummary(t *testing.T) {
	sm := newTestServiceManager(t)
	a := &testManagedService{}
	b := &testManagedService{}
	a.SetName("beta")
	b.SetName("alpha")
	sm.runServices = []inf.IService{a, b}

	summary := sm.GetRuntimeSummary()
	if summary.ServiceCount != 2 {
		t.Fatalf("expected service count 2, got %d", summary.ServiceCount)
	}
	if len(summary.ServiceNames) != 2 {
		t.Fatalf("expected 2 service names, got %d", len(summary.ServiceNames))
	}
	if summary.ServiceNames[0] != "alpha" || summary.ServiceNames[1] != "beta" {
		t.Fatalf("expected sorted names [alpha beta], got %#v", summary.ServiceNames)
	}
}

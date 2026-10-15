package mailbox

import (
	"context"
	"testing"

	"github.com/alibaba/sentinel-golang/core/circuitbreaker"
	"github.com/alibaba/sentinel-golang/core/flow"
	"github.com/alibaba/sentinel-golang/core/system"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

func TestSentinelJobTypeRulesLoadToRuntimeResources(t *testing.T) {
	serviceName := "sentinel-jobtype-load"
	resources := append([]string{serviceName}, sentinelJobTypeResources(serviceName)...)
	clearSentinelTestRules(resources...)
	defer clearSentinelTestRules(resources...)

	m := NewSentinelMiddlewareWithJobType(serviceName,
		WithFlowRule(1000),
		WithCircuitBreakerRule(0.5),
	)
	m.OnStart()
	defer m.OnStop()

	rpcResource := sentinelJobTypeResource(serviceName, def.MailboxJobTypeRpc)
	flowRules := flow.GetRulesOfResource(rpcResource)
	if len(flowRules) != 1 {
		t.Fatalf("flow rules for %s = %d, want 1", rpcResource, len(flowRules))
	}
	if flowRules[0].Resource != rpcResource || flowRules[0].Threshold != 1000 {
		t.Fatalf("unexpected flow rule: %+v", flowRules[0])
	}

	breakerRules := circuitbreaker.GetRulesOfResource(rpcResource)
	if len(breakerRules) != 1 {
		t.Fatalf("circuit breaker rules for %s = %d, want 1", rpcResource, len(breakerRules))
	}
	if breakerRules[0].Resource != rpcResource || breakerRules[0].Threshold != 0.5 {
		t.Fatalf("unexpected circuit breaker rule: %+v", breakerRules[0])
	}
}

func TestSentinelJobTypeSpecificRules(t *testing.T) {
	serviceName := "sentinel-jobtype-specific"
	resources := append([]string{serviceName}, sentinelJobTypeResources(serviceName)...)
	clearSentinelTestRules(resources...)
	defer clearSentinelTestRules(resources...)

	m := NewSentinelMiddlewareWithJobType(serviceName,
		WithJobTypeFlowRule(def.MailboxJobTypeRpc, 1000),
		WithJobTypeFlowRule(def.MailboxJobTypeTimer, 100),
		WithJobTypeErrorCountRule(def.MailboxJobTypeRpc, 3),
	)
	m.OnStart()
	defer m.OnStop()

	rpcResource := sentinelJobTypeResource(serviceName, def.MailboxJobTypeRpc)
	timerResource := sentinelJobTypeResource(serviceName, def.MailboxJobTypeTimer)
	eventResource := sentinelJobTypeResource(serviceName, def.MailboxJobTypeEvent)

	assertFlowThreshold(t, rpcResource, 1000)
	assertFlowThreshold(t, timerResource, 100)
	if rules := flow.GetRulesOfResource(eventResource); len(rules) != 0 {
		t.Fatalf("flow rules for %s = %d, want 0: %+v", eventResource, len(rules), rules)
	}

	breakerRules := circuitbreaker.GetRulesOfResource(rpcResource)
	if len(breakerRules) != 1 {
		t.Fatalf("circuit breaker rules for %s = %d, want 1", rpcResource, len(breakerRules))
	}
	if breakerRules[0].Strategy != circuitbreaker.ErrorCount || breakerRules[0].Threshold != 3 {
		t.Fatalf("unexpected rpc circuit breaker rule: %+v", breakerRules[0])
	}
	if rules := circuitbreaker.GetRulesOfResource(timerResource); len(rules) != 0 {
		t.Fatalf("circuit breaker rules for %s = %d, want 0: %+v", timerResource, len(rules), rules)
	}
}

func TestSentinelOnStopClearsFineGrainedRules(t *testing.T) {
	serviceName := "sentinel-jobtype-clear"
	resources := append([]string{serviceName}, sentinelJobTypeResources(serviceName)...)
	clearSentinelTestRules(resources...)
	defer clearSentinelTestRules(resources...)

	m := NewSentinelMiddlewareWithJobType(serviceName,
		WithFlowRule(1000),
		WithJobTypeErrorCountRule(def.MailboxJobTypeRpc, 3),
	)
	m.OnStart()
	m.OnStop()

	for _, resource := range resources {
		if rules := flow.GetRulesOfResource(resource); len(rules) != 0 {
			t.Fatalf("flow rules for %s after stop = %d, want 0", resource, len(rules))
		}
		if rules := circuitbreaker.GetRulesOfResource(resource); len(rules) != 0 {
			t.Fatalf("circuit breaker rules for %s after stop = %d, want 0", resource, len(rules))
		}
	}
}

func TestSentinelRulesMergeAcrossMiddlewareInstances(t *testing.T) {
	serviceName := "sentinel-merge-instances"
	clearSentinelTestRules(serviceName)
	defer clearSentinelTestRules(serviceName)

	m1 := NewSentinelMiddleware(serviceName,
		WithFlowRule(100),
		WithErrorCountRule(3),
	)
	m2 := NewSentinelMiddleware(serviceName,
		WithFlowRule(200),
		WithErrorCountRule(5),
	)

	m1.OnStart()
	t.Cleanup(m1.OnStop)
	m2.OnStart()
	t.Cleanup(m2.OnStop)

	flowRules := flow.GetRulesOfResource(serviceName)
	if len(flowRules) != 2 {
		t.Fatalf("flow rules after two starts = %d, want 2: %+v", len(flowRules), flowRules)
	}
	breakerRules := circuitbreaker.GetRulesOfResource(serviceName)
	if len(breakerRules) != 2 {
		t.Fatalf("breaker rules after two starts = %d, want 2: %+v", len(breakerRules), breakerRules)
	}

	m2.OnStop()
	flowRules = flow.GetRulesOfResource(serviceName)
	if len(flowRules) != 1 || flowRules[0].Threshold != 100 {
		t.Fatalf("flow rules after stopping m2 = %+v, want only m1 threshold=100", flowRules)
	}
	breakerRules = circuitbreaker.GetRulesOfResource(serviceName)
	if len(breakerRules) != 1 || breakerRules[0].Threshold != 3 {
		t.Fatalf("breaker rules after stopping m2 = %+v, want only m1 threshold=3", breakerRules)
	}

	m1.OnStop()
	if rules := flow.GetRulesOfResource(serviceName); len(rules) != 0 {
		t.Fatalf("flow rules after stopping all = %d, want 0", len(rules))
	}
	if rules := circuitbreaker.GetRulesOfResource(serviceName); len(rules) != 0 {
		t.Fatalf("breaker rules after stopping all = %d, want 0", len(rules))
	}
}

func TestSentinelSystemRulesMergeAcrossMiddlewareInstances(t *testing.T) {
	serviceName := "sentinel-system-merge"
	clearSentinelTestRules(serviceName)
	defer clearSentinelTestRules(serviceName)

	m1 := NewSentinelMiddleware(serviceName,
		WithSystemRule(WithCPUThreshold(0.70)),
	)
	m2 := NewSentinelMiddleware(serviceName,
		WithSystemRule(WithLoadThreshold(1.25)),
	)

	m1.OnStart()
	t.Cleanup(m1.OnStop)
	m2.OnStart()
	t.Cleanup(m2.OnStop)

	assertSystemRuleCount(t, 2)

	m2.OnStop()
	rules := system.GetRules()
	if len(rules) != 1 {
		t.Fatalf("system rules after stopping m2 = %d, want 1: %+v", len(rules), rules)
	}
	if rules[0].MetricType != system.CpuUsage || rules[0].TriggerCount != 0.70 {
		t.Fatalf("unexpected remaining system rule: %+v", rules[0])
	}

	m1.OnStop()
	assertSystemRuleCount(t, 0)
}

func assertFlowThreshold(t *testing.T, resource string, threshold float64) {
	t.Helper()
	rules := flow.GetRulesOfResource(resource)
	if len(rules) != 1 {
		t.Fatalf("flow rules for %s = %d, want 1", resource, len(rules))
	}
	if rules[0].Resource != resource || rules[0].Threshold != threshold {
		t.Fatalf("unexpected flow rule for %s: %+v", resource, rules[0])
	}
}

func clearSentinelTestRules(resources ...string) {
	sentinelRulesMu.Lock()
	sentinelFlowRules = make(map[string]map[string][]*flow.Rule)
	sentinelBreakerRules = make(map[string]map[string][]*circuitbreaker.Rule)
	sentinelSystemRules = make(map[string][]*system.Rule)
	sentinelRulesMu.Unlock()

	for _, resource := range resources {
		_ = flow.ClearRulesOfResource(resource)
		_ = circuitbreaker.ClearRulesOfResource(resource)
	}
	_ = system.ClearRules()
}

func assertSystemRuleCount(t *testing.T, want int) {
	t.Helper()
	if rules := system.GetRules(); len(rules) != want {
		t.Fatalf("system rules = %d, want %d: %+v", len(rules), want, rules)
	}
}

// ========== R4-M2: WithJobType*Rule 自动启用 job-type resource 模式 ==========

func TestWithJobTypeFlowRule_AutoEnablesResourceFunc(t *testing.T) {
	serviceName := "svc-auto-jobtype"
	customType := def.MailboxJobType(100)
	resources := append([]string{serviceName}, sentinelJobTypeResources(serviceName)...)
	resources = append(resources, sentinelJobTypeResource(serviceName, customType))
	clearSentinelTestRules(resources...)
	defer clearSentinelTestRules(resources...)

	m := NewSentinelMiddleware(serviceName,
		WithJobTypeFlowRule(customType, 500),
	)

	// 验证 jobTypeResourceMode 被设置
	if !m.jobTypeResourceMode {
		t.Fatal("jobTypeResourceMode should be true after WithJobTypeFlowRule")
	}

	// 验证 resourceFunc 被自动安装
	if m.resourceFunc == nil {
		t.Fatal("resourceFunc should be auto-installed when jobTypeResourceMode is true")
	}

	// 验证 effectiveRuleResources 包含 custom type resource
	effective := m.effectiveRuleResources()
	customResource := sentinelJobTypeResource(serviceName, customType)
	found := false
	for _, r := range effective {
		if r == customResource {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("effectiveRuleResources should contain %s, got %v", customResource, effective)
	}
}

func TestWithJobTypeCircuitBreakerRule_AutoEnablesResourceFunc(t *testing.T) {
	m := NewSentinelMiddleware("svc-cb",
		WithJobTypeCircuitBreakerRule(def.MailboxJobTypeRpc, 0.5),
	)
	if !m.jobTypeResourceMode {
		t.Fatal("jobTypeResourceMode should be true after WithJobTypeCircuitBreakerRule")
	}
	if m.resourceFunc == nil {
		t.Fatal("resourceFunc should be auto-installed")
	}
}

func TestWithJobTypeSlowRatioRule_AutoEnablesResourceFunc(t *testing.T) {
	m := NewSentinelMiddleware("svc-slow",
		WithJobTypeSlowRatioRule(def.MailboxJobTypeTimer, 0.8, 1000),
	)
	if !m.jobTypeResourceMode {
		t.Fatal("jobTypeResourceMode should be true after WithJobTypeSlowRatioRule")
	}
	if m.resourceFunc == nil {
		t.Fatal("resourceFunc should be auto-installed")
	}
}

func TestWithJobTypeErrorCountRule_AutoEnablesResourceFunc(t *testing.T) {
	m := NewSentinelMiddleware("svc-err",
		WithJobTypeErrorCountRule(def.MailboxJobTypeEvent, 10),
	)
	if !m.jobTypeResourceMode {
		t.Fatal("jobTypeResourceMode should be true after WithJobTypeErrorCountRule")
	}
	if m.resourceFunc == nil {
		t.Fatal("resourceFunc should be auto-installed")
	}
}

func TestPlainFlowRule_DoesNotEnableJobTypeMode(t *testing.T) {
	m := NewSentinelMiddleware("svc-plain",
		WithFlowRule(1000),
	)
	if m.jobTypeResourceMode {
		t.Fatal("jobTypeResourceMode should be false with plain WithFlowRule")
	}
	if m.resourceFunc != nil {
		t.Fatal("resourceFunc should remain nil with plain WithFlowRule")
	}
}

func TestCustomResourceFunc_NotOverridden(t *testing.T) {
	customFn := func(mctx inf.IMiddlewareContext) string { return "custom" }
	m := NewSentinelMiddleware("svc-custom",
		WithResourceFunc(customFn),
		WithJobTypeFlowRule(def.MailboxJobTypeRpc, 100),
	)
	if m.resourceFunc == nil {
		t.Fatal("resourceFunc should not be nil")
	}
	// resourceFunc 应该是用户自定义的，而非被覆盖
	mctx := NewMiddlewareContext(context.Background(), nil, "svc-custom")
	if m.resourceFunc(mctx) != "custom" {
		t.Fatal("user-defined resourceFunc should not be overridden by jobTypeResourceMode")
	}
}

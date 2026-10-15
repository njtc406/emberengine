// Package mailbox
// @Title  Sentinel 限流熔断中间件
// @Description  基于阿里 Sentinel 的限流熔断中间件，整合流量控制和熔断降级
// @Author  yr  2026/1/30
// @Update  yr  2026/1/30
package mailbox

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

	sentinel "github.com/alibaba/sentinel-golang/api"
	"github.com/alibaba/sentinel-golang/core/base"
	"github.com/alibaba/sentinel-golang/core/circuitbreaker"
	"github.com/alibaba/sentinel-golang/core/flow"
	"github.com/alibaba/sentinel-golang/core/system"

	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
)

// sentinelEntryKey 是 MiddlewareContext 中保存 Sentinel entry 的私有 key，
// 使用包内不可导出常量避免与业务 key 冲突。
const sentinelEntryKey = "__sentinel_entry__"

var (
	// ErrSentinelBlocked Sentinel 限流/熔断错误
	ErrSentinelBlocked = errors.New("blocked by sentinel")

	// sentinelInitOnce 确保 Sentinel 只初始化一次
	sentinelInitOnce sync.Once
	sentinelInitErr  error

	// Sentinel 规则按中间件实例 owner 聚合加载。
	// Sentinel SDK 的 LoadRulesOfResource 会替换该 resource 的旧规则，因此不能由
	// 每个 SentinelMiddleware 实例直接覆盖式加载；Start/Stop 统一进入注册表，
	// 对同一 resource 合并所有 owner 的规则后重新加载。
	sentinelRuleOwnerSeq atomic.Uint64
	sentinelRulesMu      sync.Mutex
	sentinelFlowRules    = make(map[string]map[string][]*flow.Rule)
	sentinelBreakerRules = make(map[string]map[string][]*circuitbreaker.Rule)
	sentinelSystemRules  = make(map[string][]*system.Rule)
)

// reloadSentinelSystemRulesLocked 合并所有 service 的 system rules 后重新加载。
// 必须在 sentinelRulesMu 持有时调用。
//
// 合并策略：按 (MetricType, Strategy, TriggerCount) 三元组去重；owner key 排序后
// 顺序稳定，相同语义规则只保留一个。
func reloadSentinelSystemRulesLocked(logger log.ILoggerX) {
	type ruleKey struct {
		metric   system.MetricType
		strategy system.AdaptiveStrategy
		trigger  float64
	}
	seen := make(map[ruleKey]struct{})
	merged := make([]*system.Rule, 0, 8)
	owners := sortedSystemRuleOwnersLocked()
	for _, owner := range owners {
		for _, r := range sentinelSystemRules[owner] {
			k := ruleKey{r.MetricType, r.Strategy, r.TriggerCount}
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			merged = append(merged, r)
		}
	}
	if _, err := system.LoadRules(merged); err != nil && logger != nil {
		logger.Errorf("Sentinel reload system rules failed (owners=%d, merged=%d): %v",
			len(sentinelSystemRules), len(merged), err)
	}
}

func sortedSystemRuleOwnersLocked() []string {
	owners := make([]string, 0, len(sentinelSystemRules))
	for owner := range sentinelSystemRules {
		owners = append(owners, owner)
	}
	sort.Strings(owners)
	return owners
}

func sortedFlowRuleOwnersLocked(resource string) []string {
	owners := make([]string, 0, len(sentinelFlowRules))
	for owner, byResource := range sentinelFlowRules {
		if len(byResource[resource]) > 0 {
			owners = append(owners, owner)
		}
	}
	sort.Strings(owners)
	return owners
}

func sortedBreakerRuleOwnersLocked(resource string) []string {
	owners := make([]string, 0, len(sentinelBreakerRules))
	for owner, byResource := range sentinelBreakerRules {
		if len(byResource[resource]) > 0 {
			owners = append(owners, owner)
		}
	}
	sort.Strings(owners)
	return owners
}

func reloadSentinelFlowRulesLocked(resource string, logger log.ILoggerX) {
	merged := make([]*flow.Rule, 0, 8)
	for _, owner := range sortedFlowRuleOwnersLocked(resource) {
		merged = append(merged, cloneFlowRulesForResource(sentinelFlowRules[owner][resource], resource)...)
	}
	if len(merged) == 0 {
		if err := flow.ClearRulesOfResource(resource); err != nil && logger != nil {
			logger.Warnf("Sentinel clear flow rules failed: resource=%s err=%v", resource, err)
		}
		return
	}
	if _, err := flow.LoadRulesOfResource(resource, merged); err != nil && logger != nil {
		logger.Errorf("Sentinel load flow rules failed: resource=%s err=%v", resource, err)
	}
}

func reloadSentinelBreakerRulesLocked(resource string, logger log.ILoggerX) {
	merged := make([]*circuitbreaker.Rule, 0, 8)
	for _, owner := range sortedBreakerRuleOwnersLocked(resource) {
		merged = append(merged, cloneCircuitBreakerRulesForResource(sentinelBreakerRules[owner][resource], resource)...)
	}
	if len(merged) == 0 {
		if err := circuitbreaker.ClearRulesOfResource(resource); err != nil && logger != nil {
			logger.Warnf("Sentinel clear circuit breaker rules failed: resource=%s err=%v", resource, err)
		}
		return
	}
	if _, err := circuitbreaker.LoadRulesOfResource(resource, merged); err != nil && logger != nil {
		logger.Errorf("Sentinel load circuit breaker rules failed: resource=%s err=%v", resource, err)
	}
}

// SentinelMiddleware 基于阿里 Sentinel 的限流熔断中间件
//
// 功能：
//   - 流量控制：QPS 限流、并发数限流、排队等待
//   - 熔断降级：错误率熔断、慢调用熔断
//   - 系统保护：CPU、Load、内存自适应保护
//
// 使用方式：
//
//	sentinel := NewSentinelMiddleware("my-service",
//	    WithFlowRule(1000),           // QPS 限流 1000
//	    WithCircuitBreakerRule(0.5),  // 50% 错误率熔断
//	)
//	mailbox.AddMiddleware(sentinel)
type SentinelMiddleware struct {
	logger      log.ILoggerX
	serviceName string
	owner       string

	// 配置
	flowRules                     []*flow.Rule
	circuitBreakerRules           []*circuitbreaker.Rule
	flowRulesByResource           map[string][]*flow.Rule
	circuitBreakerRulesByResource map[string][]*circuitbreaker.Rule
	systemRules                   []*system.Rule

	// 可选：跳过检查的条件
	skipFunc func(mctx inf.IMiddlewareContext) bool

	// 细粒度资源名生成器（可选）
	// 默认使用 serviceName，可自定义为 serviceName + jobType 等
	resourceFunc func(mctx inf.IMiddlewareContext) string

	// 规则需要加载到的静态资源列表。默认仅 serviceName；当 resourceFunc 会返回
	// serviceName 之外的资源（如 serviceName:jobType）时，必须在这里声明这些资源，
	// 否则 Sentinel 的精确 resource 匹配不会命中规则。
	ruleResources []string

	// jobTypeResourceMode 标记是否使用了 WithJobType*Rule。
	// 为 true 时，NewSentinelMiddleware 会自动安装 job-type resourceFunc（如果用户未自定义）。
	jobTypeResourceMode bool
}

// SentinelOption 配置选项
type SentinelOption func(*SentinelMiddleware)

// WithSentinelLogger 设置日志器
func WithSentinelLogger(logger log.ILoggerX) SentinelOption {
	return func(m *SentinelMiddleware) {
		m.logger = logger
	}
}

// WithFlowRule 添加流量控制规则
//
// 参数：
//   - threshold: QPS 阈值
//   - opts: 可选配置（控制行为等）
func WithFlowRule(threshold float64, opts ...FlowRuleOption) SentinelOption {
	return func(m *SentinelMiddleware) {
		m.flowRules = append(m.flowRules, newFlowRule(m.serviceName, threshold, opts...))
	}
}

// WithResourceFlowRule 为指定 resource 添加流控规则。
//
// 与 WithResourceFunc 搭配使用时，应优先使用本选项表达细粒度规则，确保
// Sentinel.Entry(resource) 与 LoadRulesOfResource(resource, rules) 精确匹配。
func WithResourceFlowRule(resource string, threshold float64, opts ...FlowRuleOption) SentinelOption {
	return func(m *SentinelMiddleware) {
		m.addFlowRulesForResource(resource, newFlowRule(resource, threshold, opts...))
	}
}

// WithJobTypeFlowRule 为 serviceName:jobType 添加流控规则。
func WithJobTypeFlowRule(jobType def.MailboxJobType, threshold float64, opts ...FlowRuleOption) SentinelOption {
	return func(m *SentinelMiddleware) {
		m.jobTypeResourceMode = true
		resource := sentinelJobTypeResource(m.serviceName, jobType)
		m.addFlowRulesForResource(resource, newFlowRule(resource, threshold, opts...))
	}
}

// WithSentinelFlowRulesForResource 直接注册完整 flow.Rule 到指定 resource。
// 传入规则会被复制，Resource 字段由 resource 参数统一覆盖。
func WithSentinelFlowRulesForResource(resource string, rules ...*flow.Rule) SentinelOption {
	return func(m *SentinelMiddleware) {
		m.addFlowRulesForResource(resource, rules...)
	}
}

// FlowRuleOption 流量规则选项
type FlowRuleOption func(*flow.Rule)

// WithThrottling 设置排队等待模式（而非直接拒绝）
func WithThrottling(maxQueueingTimeMs uint32) FlowRuleOption {
	return func(r *flow.Rule) {
		r.ControlBehavior = flow.Throttling
		r.MaxQueueingTimeMs = maxQueueingTimeMs
	}
}

// WithWarmUp 设置预热模式
func WithWarmUp(warmUpPeriodSec uint32, coldFactor uint32) FlowRuleOption {
	return func(r *flow.Rule) {
		r.TokenCalculateStrategy = flow.WarmUp
		r.WarmUpPeriodSec = warmUpPeriodSec
		r.WarmUpColdFactor = coldFactor
	}
}

// WithCircuitBreakerRule 添加熔断规则（错误率模式）
//
// 参数：
//   - errorRatioThreshold: 错误率阈值（0.0-1.0）
func WithCircuitBreakerRule(errorRatioThreshold float64) SentinelOption {
	return func(m *SentinelMiddleware) {
		m.circuitBreakerRules = append(m.circuitBreakerRules, newCircuitBreakerRule(m.serviceName, circuitbreaker.ErrorRatio, errorRatioThreshold))
	}
}

// WithResourceCircuitBreakerRule 为指定 resource 添加错误率熔断规则。
func WithResourceCircuitBreakerRule(resource string, errorRatioThreshold float64) SentinelOption {
	return func(m *SentinelMiddleware) {
		m.addCircuitBreakerRulesForResource(resource, newCircuitBreakerRule(resource, circuitbreaker.ErrorRatio, errorRatioThreshold))
	}
}

// WithJobTypeCircuitBreakerRule 为 serviceName:jobType 添加错误率熔断规则。
func WithJobTypeCircuitBreakerRule(jobType def.MailboxJobType, errorRatioThreshold float64) SentinelOption {
	return func(m *SentinelMiddleware) {
		m.jobTypeResourceMode = true
		resource := sentinelJobTypeResource(m.serviceName, jobType)
		m.addCircuitBreakerRulesForResource(resource, newCircuitBreakerRule(resource, circuitbreaker.ErrorRatio, errorRatioThreshold))
	}
}

// WithSentinelCircuitBreakerRulesForResource 直接注册完整 circuitbreaker.Rule 到指定 resource。
// 传入规则会被复制，Resource 字段由 resource 参数统一覆盖。
func WithSentinelCircuitBreakerRulesForResource(resource string, rules ...*circuitbreaker.Rule) SentinelOption {
	return func(m *SentinelMiddleware) {
		m.addCircuitBreakerRulesForResource(resource, rules...)
	}
}

// WithSlowRatioRule 添加慢调用熔断规则
//
// 参数：
//   - slowRatioThreshold: 慢调用比例阈值（0.0-1.0）
//   - maxAllowedRtMs: 慢调用 RT 阈值（毫秒）
func WithSlowRatioRule(slowRatioThreshold float64, maxAllowedRtMs uint64) SentinelOption {
	return func(m *SentinelMiddleware) {
		rule := newCircuitBreakerRule(m.serviceName, circuitbreaker.SlowRequestRatio, slowRatioThreshold)
		rule.MaxAllowedRtMs = maxAllowedRtMs
		m.circuitBreakerRules = append(m.circuitBreakerRules, rule)
	}
}

// WithJobTypeSlowRatioRule 为 serviceName:jobType 添加慢调用熔断规则。
func WithJobTypeSlowRatioRule(jobType def.MailboxJobType, slowRatioThreshold float64, maxAllowedRtMs uint64) SentinelOption {
	return func(m *SentinelMiddleware) {
		m.jobTypeResourceMode = true
		resource := sentinelJobTypeResource(m.serviceName, jobType)
		rule := newCircuitBreakerRule(resource, circuitbreaker.SlowRequestRatio, slowRatioThreshold)
		rule.MaxAllowedRtMs = maxAllowedRtMs
		m.addCircuitBreakerRulesForResource(resource, rule)
	}
}

// WithErrorCountRule 添加错误数熔断规则
//
// 参数：
//   - errorCountThreshold: 错误数阈值
func WithErrorCountRule(errorCountThreshold uint64) SentinelOption {
	return func(m *SentinelMiddleware) {
		m.circuitBreakerRules = append(m.circuitBreakerRules, newCircuitBreakerRule(m.serviceName, circuitbreaker.ErrorCount, float64(errorCountThreshold)))
	}
}

// WithJobTypeErrorCountRule 为 serviceName:jobType 添加错误数熔断规则。
func WithJobTypeErrorCountRule(jobType def.MailboxJobType, errorCountThreshold uint64) SentinelOption {
	return func(m *SentinelMiddleware) {
		m.jobTypeResourceMode = true
		resource := sentinelJobTypeResource(m.serviceName, jobType)
		m.addCircuitBreakerRulesForResource(resource, newCircuitBreakerRule(resource, circuitbreaker.ErrorCount, float64(errorCountThreshold)))
	}
}

// WithSystemRule 添加系统保护规则
func WithSystemRule(opts ...SystemRuleOption) SentinelOption {
	return func(m *SentinelMiddleware) {
		rule := &system.Rule{
			MetricType:   system.Load,
			TriggerCount: 0.8, // 默认 80% 负载
			Strategy:     system.BBR,
		}
		for _, opt := range opts {
			opt(rule)
		}
		m.systemRules = append(m.systemRules, rule)
	}
}

// SystemRuleOption 系统规则选项
type SystemRuleOption func(*system.Rule)

// WithCPUThreshold 设置 CPU 使用率阈值
func WithCPUThreshold(threshold float64) SystemRuleOption {
	return func(r *system.Rule) {
		r.MetricType = system.CpuUsage
		r.TriggerCount = threshold
	}
}

// WithLoadThreshold 设置系统负载阈值
func WithLoadThreshold(threshold float64) SystemRuleOption {
	return func(r *system.Rule) {
		r.MetricType = system.Load
		r.TriggerCount = threshold
	}
}

// WithSentinelSkipFunc 设置跳过检查的条件
func WithSentinelSkipFunc(fn func(mctx inf.IMiddlewareContext) bool) SentinelOption {
	return func(m *SentinelMiddleware) {
		m.skipFunc = fn
	}
}

// WithResourceFunc 设置自定义资源名生成器
//
// 可用于更细粒度的限流，如按 jobType 区分
func WithResourceFunc(fn func(mctx inf.IMiddlewareContext) string) SentinelOption {
	return func(m *SentinelMiddleware) {
		m.resourceFunc = fn
	}
}

// WithSentinelRuleResources 声明 flow / circuit breaker 规则要加载到的资源名。
//
// 当 WithResourceFunc 返回动态资源时，Sentinel 不会把 serviceName 规则自动应用到
// 子资源；调用方应把所有可能的静态资源列在这里。serviceName 始终会被自动加入。
func WithSentinelRuleResources(resources ...string) SentinelOption {
	return func(m *SentinelMiddleware) {
		m.ruleResources = append(m.ruleResources, resources...)
	}
}

func newFlowRule(resource string, threshold float64, opts ...FlowRuleOption) *flow.Rule {
	rule := &flow.Rule{
		Resource:               resource,
		TokenCalculateStrategy: flow.Direct,
		ControlBehavior:        flow.Reject,
		Threshold:              threshold,
		StatIntervalInMs:       1000,
	}
	for _, opt := range opts {
		opt(rule)
	}
	return rule
}

func newCircuitBreakerRule(resource string, strategy circuitbreaker.Strategy, threshold float64) *circuitbreaker.Rule {
	return &circuitbreaker.Rule{
		Resource:         resource,
		Strategy:         strategy,
		RetryTimeoutMs:   5000,
		MinRequestAmount: 10,
		StatIntervalMs:   10000,
		Threshold:        threshold,
	}
}

func (m *SentinelMiddleware) addFlowRulesForResource(resource string, rules ...*flow.Rule) {
	if resource == "" {
		return
	}
	m.ruleResources = append(m.ruleResources, resource)
	m.flowRulesByResource[resource] = append(m.flowRulesByResource[resource], cloneFlowRulesForResource(rules, resource)...)
}

func (m *SentinelMiddleware) addCircuitBreakerRulesForResource(resource string, rules ...*circuitbreaker.Rule) {
	if resource == "" {
		return
	}
	m.ruleResources = append(m.ruleResources, resource)
	m.circuitBreakerRulesByResource[resource] = append(m.circuitBreakerRulesByResource[resource], cloneCircuitBreakerRulesForResource(rules, resource)...)
}

// NewSentinelMiddleware 创建 Sentinel 中间件
func NewSentinelMiddleware(serviceName string, opts ...SentinelOption) *SentinelMiddleware {
	owner := fmt.Sprintf("%s#%d", serviceName, sentinelRuleOwnerSeq.Add(1))
	m := &SentinelMiddleware{
		serviceName:                   serviceName,
		owner:                         owner,
		flowRules:                     make([]*flow.Rule, 0),
		circuitBreakerRules:           make([]*circuitbreaker.Rule, 0),
		flowRulesByResource:           make(map[string][]*flow.Rule),
		circuitBreakerRulesByResource: make(map[string][]*circuitbreaker.Rule),
		systemRules:                   make([]*system.Rule, 0),
	}
	for _, opt := range opts {
		opt(m)
	}

	// 当使用了 WithJobType*Rule 时，自动安装 job-type resource 映射。
	// 仅在 resourceFunc 未被用户/NewSentinelMiddlewareWithJobType 设置时执行，
	// 避免与已有 ruleResources 重复追加（effectiveRuleResources 会去重，但减少冗余分配）。
	if m.jobTypeResourceMode && m.resourceFunc == nil {
		m.ruleResources = append(m.ruleResources, sentinelJobTypeResources(serviceName)...)
		m.resourceFunc = func(mctx inf.IMiddlewareContext) string {
			job := mctx.Job()
			if job != nil {
				return sentinelJobTypeResource(serviceName, job.GetType())
			}
			return serviceName
		}
	}

	return m
}

func (m *SentinelMiddleware) Name() string {
	return "Sentinel"
}

func (m *SentinelMiddleware) OnStart() {
	// 全局初始化 Sentinel（只执行一次）
	sentinelInitOnce.Do(func() {
		sentinelInitErr = sentinel.InitDefault()
	})

	if sentinelInitErr != nil {
		if m.logger != nil {
			m.logger.Errorf("Sentinel init failed: %v", sentinelInitErr)
		}
		return
	}

	ruleResources := m.effectiveRuleResources()
	if m.resourceFunc != nil && len(m.ruleResources) == 0 && (len(m.flowRules) > 0 || len(m.circuitBreakerRules) > 0) && m.logger != nil {
		m.logger.Warnf("Sentinel resourceFunc is set for service=%s, but no rule resources were declared; rules only apply to resource=%s",
			m.serviceName, m.serviceName)
	}

	// 加载流量控制规则：按 owner 注册，再按 resource 聚合重载。
	if len(m.flowRules) > 0 || len(m.flowRulesByResource) > 0 {
		byResource := make(map[string][]*flow.Rule, len(ruleResources))
		for _, resource := range ruleResources {
			rules := m.flowRulesForResource(resource)
			if len(rules) == 0 {
				continue
			}
			byResource[resource] = rules
		}
		if len(byResource) > 0 {
			sentinelRulesMu.Lock()
			sentinelFlowRules[m.owner] = byResource
			for resource := range byResource {
				reloadSentinelFlowRulesLocked(resource, m.logger)
			}
			sentinelRulesMu.Unlock()
		}
	}

	// 加载熔断规则：按 owner 注册，再按 resource 聚合重载。
	if len(m.circuitBreakerRules) > 0 || len(m.circuitBreakerRulesByResource) > 0 {
		byResource := make(map[string][]*circuitbreaker.Rule, len(ruleResources))
		for _, resource := range ruleResources {
			rules := m.circuitBreakerRulesForResource(resource)
			if len(rules) == 0 {
				continue
			}
			byResource[resource] = rules
		}
		if len(byResource) > 0 {
			sentinelRulesMu.Lock()
			sentinelBreakerRules[m.owner] = byResource
			for resource := range byResource {
				reloadSentinelBreakerRulesLocked(resource, m.logger)
			}
			sentinelRulesMu.Unlock()
		}
	}

	// 加载系统保护规则：合并到全局表后由 reload 重新整体加载
	if len(m.systemRules) > 0 {
		sentinelRulesMu.Lock()
		sentinelSystemRules[m.owner] = m.systemRules
		reloadSentinelSystemRulesLocked(m.logger)
		sentinelRulesMu.Unlock()
	}

	if m.logger != nil {
		m.logger.Infof("SentinelMiddleware started: service=%s, resources=%d, flowRules=%d, breakerRules=%d, systemRules=%d",
			m.serviceName, len(ruleResources), m.flowRuleCount(), m.circuitBreakerRuleCount(), len(m.systemRules))
	}
}

func (m *SentinelMiddleware) OnStop() {
	ruleResources := m.effectiveRuleResources()
	// 卸载流量控制规则：只移除本 owner，再按 resource 聚合重载剩余规则。
	if len(m.flowRules) > 0 || len(m.flowRulesByResource) > 0 {
		sentinelRulesMu.Lock()
		delete(sentinelFlowRules, m.owner)
		for _, resource := range ruleResources {
			reloadSentinelFlowRulesLocked(resource, m.logger)
		}
		sentinelRulesMu.Unlock()
	}
	// 卸载熔断规则：只移除本 owner，再按 resource 聚合重载剩余规则。
	if len(m.circuitBreakerRules) > 0 || len(m.circuitBreakerRulesByResource) > 0 {
		sentinelRulesMu.Lock()
		delete(sentinelBreakerRules, m.owner)
		for _, resource := range ruleResources {
			reloadSentinelBreakerRulesLocked(resource, m.logger)
		}
		sentinelRulesMu.Unlock()
	}
	// 移除本 owner 的 system rules 后重新合并加载。
	if len(m.systemRules) > 0 {
		sentinelRulesMu.Lock()
		delete(sentinelSystemRules, m.owner)
		reloadSentinelSystemRulesLocked(m.logger)
		sentinelRulesMu.Unlock()
	}
	if m.logger != nil {
		m.logger.Infof("SentinelMiddleware stopped: service=%s", m.serviceName)
	}
}

func (m *SentinelMiddleware) effectiveRuleResources() []string {
	seen := make(map[string]struct{}, len(m.ruleResources)+len(m.flowRulesByResource)+len(m.circuitBreakerRulesByResource)+1)
	resources := make([]string, 0, len(m.ruleResources)+1)
	add := func(resource string) {
		if resource == "" {
			return
		}
		if _, ok := seen[resource]; ok {
			return
		}
		seen[resource] = struct{}{}
		resources = append(resources, resource)
	}
	add(m.serviceName)
	for _, resource := range m.ruleResources {
		add(resource)
	}
	for resource := range m.flowRulesByResource {
		add(resource)
	}
	for resource := range m.circuitBreakerRulesByResource {
		add(resource)
	}
	return resources
}

func (m *SentinelMiddleware) flowRulesForResource(resource string) []*flow.Rule {
	rules := cloneFlowRulesForResource(m.flowRules, resource)
	rules = append(rules, cloneFlowRulesForResource(m.flowRulesByResource[resource], resource)...)
	return rules
}

func (m *SentinelMiddleware) circuitBreakerRulesForResource(resource string) []*circuitbreaker.Rule {
	rules := cloneCircuitBreakerRulesForResource(m.circuitBreakerRules, resource)
	rules = append(rules, cloneCircuitBreakerRulesForResource(m.circuitBreakerRulesByResource[resource], resource)...)
	return rules
}

func (m *SentinelMiddleware) flowRuleCount() int {
	total := len(m.flowRules)
	for _, rules := range m.flowRulesByResource {
		total += len(rules)
	}
	return total
}

func (m *SentinelMiddleware) circuitBreakerRuleCount() int {
	total := len(m.circuitBreakerRules)
	for _, rules := range m.circuitBreakerRulesByResource {
		total += len(rules)
	}
	return total
}

func cloneFlowRulesForResource(rules []*flow.Rule, resource string) []*flow.Rule {
	out := make([]*flow.Rule, 0, len(rules))
	for _, rule := range rules {
		if rule == nil {
			continue
		}
		cloned := *rule
		cloned.Resource = resource
		out = append(out, &cloned)
	}
	return out
}

func cloneCircuitBreakerRulesForResource(rules []*circuitbreaker.Rule, resource string) []*circuitbreaker.Rule {
	out := make([]*circuitbreaker.Rule, 0, len(rules))
	for _, rule := range rules {
		if rule == nil {
			continue
		}
		cloned := *rule
		cloned.Resource = resource
		out = append(out, &cloned)
	}
	return out
}

func (m *SentinelMiddleware) OnReceive(mctx inf.IMiddlewareContext) dto.MiddlewareResult {
	// 检查是否跳过
	if m.skipFunc != nil && m.skipFunc(mctx) {
		return dto.Continue()
	}

	// 获取资源名
	resource := m.serviceName
	if m.resourceFunc != nil {
		resource = m.resourceFunc(mctx)
	}

	// Sentinel 入口检查
	e, b := sentinel.Entry(resource, sentinel.WithTrafficType(base.Inbound))
	if b != nil {
		// 被限流或熔断
		return dto.Reject(fmt.Errorf("%w: %s blocked by %s", ErrSentinelBlocked, resource, b.BlockType().String()))
	}

	// 保存 entry 用于 OnComplete
	mctx.Set(sentinelEntryKey, e)
	return dto.Continue()
}

func (m *SentinelMiddleware) OnComplete(mctx inf.IMiddlewareContext, err error, panicVal interface{}) {
	m.exitEntry(mctx, err, panicVal)
}

func (m *SentinelMiddleware) OnFrameworkCleanup(mctx inf.IMiddlewareContext, err error, panicVal interface{}) {
	m.exitEntry(mctx, err, panicVal)
}

func (m *SentinelMiddleware) exitEntry(mctx inf.IMiddlewareContext, err error, panicVal interface{}) {
	entryVal, ok := mctx.Get(sentinelEntryKey)
	if !ok {
		return
	}

	e, ok := entryVal.(*base.SentinelEntry)
	if !ok || e == nil {
		return
	}

	// 标记错误（影响熔断统计）
	if err != nil || panicVal != nil {
		if err != nil {
			sentinel.TraceError(e, err)
		} else {
			sentinel.TraceError(e, fmt.Errorf("panic: %v", panicVal))
		}
	}

	e.Exit()
}

// ========== 便捷构造函数 ==========

// NewSimpleSentinelMiddleware 创建简单配置的 Sentinel 中间件
//
// 参数：
//   - serviceName: 服务名
//   - qps: QPS 限流阈值
//   - errorRatio: 错误率熔断阈值（0.0-1.0）
func NewSimpleSentinelMiddleware(serviceName string, qps float64, errorRatio float64, logger log.ILoggerX) *SentinelMiddleware {
	opts := []SentinelOption{
		WithSentinelLogger(logger),
	}

	if qps > 0 {
		opts = append(opts, WithFlowRule(qps))
	}

	if errorRatio > 0 {
		opts = append(opts, WithCircuitBreakerRule(errorRatio))
	}

	return NewSentinelMiddleware(serviceName, opts...)
}

// NewSentinelMiddlewareWithJobType 创建按 JobType 细分的 Sentinel 中间件
//
// 不同类型的 Job 使用不同的资源名，可以分别配置限流规则。
// 内置 JobType 会自动声明为 rule resource；业务自定义 MailboxJobType 不在
// 内置列表内，需要额外使用 WithSentinelRuleResources / WithResourceFlowRule /
// WithSentinelFlowRulesForResource 等选项显式注册对应 resource。
func NewSentinelMiddlewareWithJobType(serviceName string, opts ...SentinelOption) *SentinelMiddleware {
	allOpts := append([]SentinelOption{}, opts...)
	allOpts = append(allOpts,
		WithSentinelRuleResources(sentinelJobTypeResources(serviceName)...),
		WithResourceFunc(func(mctx inf.IMiddlewareContext) string {
			job := mctx.Job()
			if job != nil {
				return sentinelJobTypeResource(serviceName, job.GetType())
			}
			return serviceName
		}),
	)
	return NewSentinelMiddleware(serviceName, allOpts...)
}

func sentinelJobTypeResource(serviceName string, jobType def.MailboxJobType) string {
	return fmt.Sprintf("%s:%d", serviceName, jobType)
}

func sentinelJobTypeResources(serviceName string) []string {
	jobTypes := []def.MailboxJobType{
		def.MailboxJobTypeNone,
		def.MailboxJobTypeRpc,
		def.MailboxJobTypeEvent,
		def.MailboxJobTypeTimer,
		def.MailboxJobTypeConcurrentCallback,
		def.MailboxJobTypeSysCtl,
	}
	resources := make([]string, 0, len(jobTypes))
	for _, jobType := range jobTypes {
		resources = append(resources, sentinelJobTypeResource(serviceName, jobType))
	}
	return resources
}

// ========== 默认跳过函数 ==========

// DefaultSentinelSkipFunc 默认跳过检查函数
// 跳过紧急及以上优先级的消息（数值 <= PriorityUrgent）
func DefaultSentinelSkipFunc(mctx inf.IMiddlewareContext) bool {
	job := mctx.Job()
	if job == nil {
		return false
	}
	// 紧急及以上消息不限流（与 SuspendPolicy、RateLimitMiddleware 保持一致）
	return job.GetPriority() <= def.PriorityUrgent
}

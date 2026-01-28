// Package mailbox
// @Title  Sentinel 限流熔断中间件
// @Description  基于阿里 Sentinel 的限流熔断中间件，整合流量控制和熔断降级
// @Author  yr  2026/1/30
// @Update  yr  2026/1/30
package mailbox

import (
	"errors"
	"fmt"
	"sync"

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

var (
	// ErrSentinelBlocked Sentinel 限流/熔断错误
	ErrSentinelBlocked = errors.New("blocked by sentinel")

	// sentinelInitOnce 确保 Sentinel 只初始化一次
	sentinelInitOnce sync.Once
	sentinelInitErr  error
)

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

	// 配置
	flowRules           []*flow.Rule
	circuitBreakerRules []*circuitbreaker.Rule
	systemRules         []*system.Rule

	// 可选：跳过检查的条件
	skipFunc func(mctx inf.IMiddlewareContext) bool

	// 细粒度资源名生成器（可选）
	// 默认使用 serviceName，可自定义为 serviceName + jobType 等
	resourceFunc func(mctx inf.IMiddlewareContext) string
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
		rule := &flow.Rule{
			Resource:               m.serviceName,
			TokenCalculateStrategy: flow.Direct,
			ControlBehavior:        flow.Reject, // 默认直接拒绝
			Threshold:              threshold,
			StatIntervalInMs:       1000, // 1秒统计周期
		}
		for _, opt := range opts {
			opt(rule)
		}
		m.flowRules = append(m.flowRules, rule)
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
		rule := &circuitbreaker.Rule{
			Resource:         m.serviceName,
			Strategy:         circuitbreaker.ErrorRatio,
			RetryTimeoutMs:   5000,  // 熔断恢复时间 5s
			MinRequestAmount: 10,    // 最小请求数
			StatIntervalMs:   10000, // 统计周期 10s
			Threshold:        errorRatioThreshold,
		}
		m.circuitBreakerRules = append(m.circuitBreakerRules, rule)
	}
}

// WithSlowRatioRule 添加慢调用熔断规则
//
// 参数：
//   - slowRatioThreshold: 慢调用比例阈值（0.0-1.0）
//   - maxAllowedRtMs: 慢调用 RT 阈值（毫秒）
func WithSlowRatioRule(slowRatioThreshold float64, maxAllowedRtMs uint64) SentinelOption {
	return func(m *SentinelMiddleware) {
		rule := &circuitbreaker.Rule{
			Resource:         m.serviceName,
			Strategy:         circuitbreaker.SlowRequestRatio,
			RetryTimeoutMs:   5000,
			MinRequestAmount: 10,
			StatIntervalMs:   10000,
			MaxAllowedRtMs:   maxAllowedRtMs,
			Threshold:        slowRatioThreshold,
		}
		m.circuitBreakerRules = append(m.circuitBreakerRules, rule)
	}
}

// WithErrorCountRule 添加错误数熔断规则
//
// 参数：
//   - errorCountThreshold: 错误数阈值
func WithErrorCountRule(errorCountThreshold uint64) SentinelOption {
	return func(m *SentinelMiddleware) {
		rule := &circuitbreaker.Rule{
			Resource:         m.serviceName,
			Strategy:         circuitbreaker.ErrorCount,
			RetryTimeoutMs:   5000,
			MinRequestAmount: 10,
			StatIntervalMs:   10000,
			Threshold:        float64(errorCountThreshold),
		}
		m.circuitBreakerRules = append(m.circuitBreakerRules, rule)
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

// NewSentinelMiddleware 创建 Sentinel 中间件
func NewSentinelMiddleware(serviceName string, opts ...SentinelOption) *SentinelMiddleware {
	m := &SentinelMiddleware{
		serviceName:         serviceName,
		flowRules:           make([]*flow.Rule, 0),
		circuitBreakerRules: make([]*circuitbreaker.Rule, 0),
		systemRules:         make([]*system.Rule, 0),
	}
	for _, opt := range opts {
		opt(m)
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

	// 加载流量控制规则
	if len(m.flowRules) > 0 {
		// 更新资源名
		for _, rule := range m.flowRules {
			rule.Resource = m.serviceName
		}
		if _, err := flow.LoadRules(m.flowRules); err != nil {
			if m.logger != nil {
				m.logger.Errorf("Sentinel load flow rules failed: %v", err)
			}
		}
	}

	// 加载熔断规则
	if len(m.circuitBreakerRules) > 0 {
		for _, rule := range m.circuitBreakerRules {
			rule.Resource = m.serviceName
		}
		if _, err := circuitbreaker.LoadRules(m.circuitBreakerRules); err != nil {
			if m.logger != nil {
				m.logger.Errorf("Sentinel load circuit breaker rules failed: %v", err)
			}
		}
	}

	// 加载系统保护规则
	if len(m.systemRules) > 0 {
		if _, err := system.LoadRules(m.systemRules); err != nil {
			if m.logger != nil {
				m.logger.Errorf("Sentinel load system rules failed: %v", err)
			}
		}
	}

	if m.logger != nil {
		m.logger.Infof("SentinelMiddleware started: service=%s, flowRules=%d, breakerRules=%d, systemRules=%d",
			m.serviceName, len(m.flowRules), len(m.circuitBreakerRules), len(m.systemRules))
	}
}

func (m *SentinelMiddleware) OnStop() {
	if m.logger != nil {
		m.logger.Infof("SentinelMiddleware stopped: service=%s", m.serviceName)
	}
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
	mctx.Set("sentinel_entry", e)
	return dto.Continue()
}

func (m *SentinelMiddleware) OnComplete(mctx inf.IMiddlewareContext, err error, panicVal interface{}) {
	entryVal, ok := mctx.Get("sentinel_entry")
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
// 不同类型的 Job 使用不同的资源名，可以分别配置限流规则
func NewSentinelMiddlewareWithJobType(serviceName string, opts ...SentinelOption) *SentinelMiddleware {
	allOpts := append(opts, WithResourceFunc(func(mctx inf.IMiddlewareContext) string {
		job := mctx.Job()
		if job != nil {
			return fmt.Sprintf("%s:%d", serviceName, job.GetType())
		}
		return serviceName
	}))
	return NewSentinelMiddleware(serviceName, allOpts...)
}

// ========== 默认跳过函数 ==========

// DefaultSentinelSkipFunc 默认跳过检查函数
// 跳过紧急优先级的消息
func DefaultSentinelSkipFunc(mctx inf.IMiddlewareContext) bool {
	job := mctx.Job()
	if job == nil {
		return false
	}
	// 紧急消息不限流
	return job.GetPriority() >= def.PriorityUrgent
}

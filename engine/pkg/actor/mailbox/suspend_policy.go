// Package mailbox
// @Title  挂起策略
// @Description  定义 Mailbox 挂起状态下的消息准入控制规则
// @Author  yr  2025/2/8
// @Update  yr  2025/2/8
package mailbox

import (
	job2 "github.com/njtc406/emberengine/engine/pkg/actor/mailbox/job"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

// DefaultSuspendPolicy 是 ISuspendPolicy 的默认实现。
//
// 挂起状态下的放行规则：
//  1. 优先级 <= PriorityUrgent（紧急及以上）的消息始终放行；
//  2. RPC Reply 消息放行，避免关闭过程中回调/等待永远不触发；
//  3. ServiceConcurrentCallback 事件放行，允许 monitor.AsyncCall 回调完成。
//
// 所有其他消息将被拒绝。
type DefaultSuspendPolicy struct{}

// NewDefaultSuspendPolicy 创建默认的挂起策略实例。
func NewDefaultSuspendPolicy() *DefaultSuspendPolicy {
	return &DefaultSuspendPolicy{}
}

// ShouldAllow 判断挂起状态下是否允许该事件通过。
//
// 参数：
//   - ctx: 上下文
//   - evt: 待判断的事件
//
// 返回：
//   - true: 允许通过
//   - false: 拒绝入队
func (p *DefaultSuspendPolicy) ShouldAllow(job inf.IMailboxJob) bool {
	// 规则1: 紧急及以上优先级始终放行
	if job.GetPriority() <= def.PriorityUrgent {
		return true
	}

	// 规则2: RPC Reply 放行
	if job.GetType() == def.MailboxJobTypeRpc {
		envelope := job2.GetJobPayloadAs[inf.IEnvelope](job)
		data := envelope.GetData()
		if data != nil && data.IsReply() {
			return true
		}
	}

	// 规则3: 并发回调事件放行
	if job.GetType() == def.MailboxJobTypeConcurrentCallback {
		return true
	}

	return false
}

// CompositeSuspendPolicy 组合多个 ISuspendPolicy，任一策略放行则放行。
// 便于用户在默认规则基础上追加自定义放行条件。
type CompositeSuspendPolicy struct {
	policies []inf.ISuspendPolicy
}

// NewCompositeSuspendPolicy 创建组合策略。
// 传入的策略将按顺序检查，任一返回 true 即放行。
func NewCompositeSuspendPolicy(policies ...inf.ISuspendPolicy) *CompositeSuspendPolicy {
	return &CompositeSuspendPolicy{policies: policies}
}

// ShouldAllow 检查所有策略，任一放行则返回 true。
func (p *CompositeSuspendPolicy) ShouldAllow(job inf.IMailboxJob) bool {
	for _, policy := range p.policies {
		if policy.ShouldAllow(job) {
			return true
		}
	}
	return false
}

// AddPolicy 动态添加策略。
func (p *CompositeSuspendPolicy) AddPolicy(policy inf.ISuspendPolicy) {
	p.policies = append(p.policies, policy)
}

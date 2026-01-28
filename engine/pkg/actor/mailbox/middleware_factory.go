// Package mailbox
// @Title  中间件工厂
// @Description  根据配置创建中间件
// @Author  yr  2025/2/8
// @Update  yr  2025/2/8
package mailbox

import (
	"time"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
)

// CreateMiddlewaresFromConfig 根据配置创建中间件列表
//
// 参数：
//   - conf: 邮箱配置
//   - logger: 日志器
//   - isDebug: 是否为 debug 模式
//
// 返回：
//   - 中间件列表
func CreateMiddlewaresFromConfig(conf *config.MailboxConf, logger log.ILoggerX, isDebug bool) []inf.IMailboxMiddleware {
	if conf == nil || conf.MiddlewareConf == nil {
		// 没有配置中间件，使用默认配置
		return createDefaultMiddlewares(logger, isDebug)
	}

	mconf := conf.MiddlewareConf
	var middlewares []inf.IMailboxMiddleware

	// 1. DispatchKey 统计中间件（仅 debug 模式）
	if isDebug && mconf.EnableDispatchKeyStats {
		interval := mconf.DispatchKeyStatsInterval
		if interval <= 0 {
			interval = 10 * time.Second
		}
		topN := mconf.DispatchKeyStatsTopN
		if topN <= 0 {
			topN = 10
		}
		middlewares = append(middlewares, NewDispatchKeyStatsMiddleware(logger, interval, topN))
	}

	// 2. 限流中间件
	if mconf.RateLimitConf != nil && mconf.RateLimitConf.Enable {
		rconf := mconf.RateLimitConf
		rate := rconf.Rate
		if rate <= 0 {
			rate = 10000
		}
		burst := rconf.Burst
		if burst <= 0 {
			burst = 1000
		}

		opts := []RateLimitOption{
			WithRateLimitLogger(logger),
		}

		// 跳过紧急消息的限流
		if rconf.SkipUrgent {
			opts = append(opts, WithRateLimitSkipFunc(func(mctx inf.IMiddlewareContext) bool {
				return mctx.Job().GetPriority() <= def.PriorityUrgent
			}))
		}

		middlewares = append(middlewares, NewRateLimitMiddleware(rate, burst, opts...))
	}

	// 3. 熔断中间件
	if mconf.CircuitBreakerConf != nil && mconf.CircuitBreakerConf.Enable {
		cconf := mconf.CircuitBreakerConf
		failureThreshold := cconf.FailureThreshold
		if failureThreshold <= 0 {
			failureThreshold = 5
		}
		successThreshold := cconf.SuccessThreshold
		if successThreshold <= 0 {
			successThreshold = 3
		}

		opts := []CircuitBreakerOption{
			WithCircuitBreakerLogger(logger),
		}

		if cconf.CooldownDuration > 0 {
			opts = append(opts, WithCooldownDuration(cconf.CooldownDuration))
		}
		if cconf.WindowDuration > 0 {
			opts = append(opts, WithWindowDuration(cconf.WindowDuration))
		}
		if cconf.HalfOpenMaxAllowed > 0 {
			opts = append(opts, WithHalfOpenMaxAllowed(cconf.HalfOpenMaxAllowed))
		}

		middlewares = append(middlewares, NewCircuitBreakerMiddleware(failureThreshold, successThreshold, opts...))
	}

	return middlewares
}

// createDefaultMiddlewares 创建默认中间件（无配置时使用）
func createDefaultMiddlewares(logger log.ILoggerX, isDebug bool) []inf.IMailboxMiddleware {
	var middlewares []inf.IMailboxMiddleware

	// debug 模式下启用 DispatchKey 统计
	if isDebug {
		middlewares = append(middlewares, NewDispatchKeyStatsMiddleware(logger, 10*time.Second, 10))
	}

	return middlewares
}

// MergeMiddlewares 合并配置中间件和用户自定义中间件
//
// 配置中间件在前，用户自定义中间件在后
func MergeMiddlewares(configMiddlewares, customMiddlewares []inf.IMailboxMiddleware) []inf.IMailboxMiddleware {
	result := make([]inf.IMailboxMiddleware, 0, len(configMiddlewares)+len(customMiddlewares))
	result = append(result, configMiddlewares...)
	result = append(result, customMiddlewares...)
	return result
}

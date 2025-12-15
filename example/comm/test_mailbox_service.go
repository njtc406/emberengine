// Package comm
// 模块名: mailbox测试服务
// 功能描述: 测试多级优先级mailbox的各种场景
// 作者:  EmberEngine
// 最后更新:  2025/11/16
package comm

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/core"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/profiler"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
)

const (
	ServiceNameMailboxTest = "MailboxTestService"
)

const (
	// 测试事件类型定义
	EventTypeSystem     int32 = 1 // 系统消息
	EventTypeUrgent     int32 = 2 // 紧急消息
	EventTypeHigh       int32 = 3 // 高优先级消息
	EventTypeNormal     int32 = 4 // 普通消息
	EventTypeLow        int32 = 5 // 低优先级消息
	EventTypeBatch      int32 = 6 // 批量消息
	EventTypeBackground int32 = 7 // 后台消息
)

// MailboxTestService 用于测试多级优先级mailbox的服务
type MailboxTestService struct {
	core.Service

	// 统计信息
	stats *MailboxTestStats
}

// MailboxTestStats 测试统计信息
type MailboxTestStats struct {
	mu sync.Mutex

	// 各优先级消息计数
	sysCount        atomic.Int64
	urgentCount     atomic.Int64
	highCount       atomic.Int64
	normalCount     atomic.Int64
	lowCount        atomic.Int64
	batchCount      atomic.Int64
	backgroundCount atomic.Int64

	// 各优先级处理时间
	sysProcessTime        atomic.Int64
	urgentProcessTime     atomic.Int64
	highProcessTime       atomic.Int64
	normalProcessTime     atomic.Int64
	lowProcessTime        atomic.Int64
	batchProcessTime      atomic.Int64
	backgroundProcessTime atomic.Int64

	// 测试开始时间
	startTime time.Time
	// 测试持续时间
	duration time.Duration
}

func (s *MailboxTestService) OnInit() error {
	s.stats = &MailboxTestStats{
		startTime: time.Now(),
	}

	// 注册各种优先级消息的处理器
	s.registerEventHandlers()

	// 配置多级优先级mailbox
	// 可以在这里测试不同的调度策略
	s.setupMailboxConfig()

	// 启动测试任务
	s.scheduleTests()

	return nil
}

// registerEventHandlers 注册事件处理器
func (s *MailboxTestService) registerEventHandlers() {
	// 注册系统消息处理器
	s.RegisterUserHandler(EventTypeSystem, func(ctx context.Context, ev inf.IEvent, _ bool, _ *profiler.Analyzer) {
		if msg, ok := ev.(*event.Event); ok {
			if data, ok := msg.Data.(string); ok {
				s.HandleSystemMessage(data)
			}
		}
	})

	// 注册紧急消息处理器
	s.RegisterUserHandler(EventTypeUrgent, func(ctx context.Context, ev inf.IEvent, _ bool, _ *profiler.Analyzer) {
		if msg, ok := ev.(*event.Event); ok {
			if data, ok := msg.Data.(string); ok {
				s.HandleUrgentMessage(data)
			}
		}
	})

	// 注册高优先级消息处理器
	s.RegisterUserHandler(EventTypeHigh, func(ctx context.Context, ev inf.IEvent, _ bool, _ *profiler.Analyzer) {
		if msg, ok := ev.(*event.Event); ok {
			if data, ok := msg.Data.(string); ok {
				s.HandleHighPriorityMessage(data)
			}
		}
	})

	// 注册普通消息处理器
	s.RegisterUserHandler(EventTypeNormal, func(ctx context.Context, ev inf.IEvent, _ bool, _ *profiler.Analyzer) {
		if msg, ok := ev.(*event.Event); ok {
			if data, ok := msg.Data.(string); ok {
				s.HandleNormalMessage(data)
			}
		}
	})

	// 注册低优先级消息处理器
	s.RegisterUserHandler(EventTypeLow, func(ctx context.Context, ev inf.IEvent, _ bool, _ *profiler.Analyzer) {
		if msg, ok := ev.(*event.Event); ok {
			if data, ok := msg.Data.(string); ok {
				s.HandleLowPriorityMessage(data)
			}
		}
	})

	// 注册批量消息处理器
	s.RegisterUserHandler(EventTypeBatch, func(ctx context.Context, ev inf.IEvent, _ bool, _ *profiler.Analyzer) {
		if msg, ok := ev.(*event.Event); ok {
			if data, ok := msg.Data.(string); ok {
				s.HandleBatchMessage(data)
			}
		}
	})

	// 注册后台消息处理器
	s.RegisterUserHandler(EventTypeBackground, func(ctx context.Context, ev inf.IEvent, _ bool, _ *profiler.Analyzer) {
		if msg, ok := ev.(*event.Event); ok {
			if data, ok := msg.Data.(string); ok {
				s.HandleBackgroundMessage(data)
			}
		}
	})
}

// setupMailboxConfig 配置mailbox
// 这里演示三种不同的策略配置
func (s *MailboxTestService) setupMailboxConfig() {
	// 可以选择不同的策略进行测试：

	// 方案1: 使用默认配置 (加权轮询策略)
	s.useDefaultConfig()

	// 方案2: 使用绝对优先策略
	//s.useAbsolutePriorityConfig()

	// 方案3: 使用防饥饿策略
	//s.useFairnessConfig()

	// 方案4: 自定义配置
	//s.useCustomConfig()
}

// useDefaultConfig 使用默认配置
func (s *MailboxTestService) useDefaultConfig() {
	// TODO: 新的统一Worker不再支持动态配置，配置应在启动时指定
	log.SysLogger.Infof("[%s] 使用默认配置（配置在启动时已指定）", s.GetName())
}

// useAbsolutePriorityConfig 使用绝对优先策略
func (s *MailboxTestService) useAbsolutePriorityConfig() {
	// TODO: 新的统一Worker不再支持动态配置，配置应在启动时指定
	log.SysLogger.Infof("[%s] 使用绝对优先策略配置（配置在启动时已指定）", s.GetName())
}

// useFairnessConfig 使用防饥饿策略
func (s *MailboxTestService) useFairnessConfig() {
	// TODO: 新的统一Worker不再支持动态配置，配置应在启动时指定
	log.SysLogger.Infof("[%s] 使用防饥饿策略配置（配置在启动时已指定）", s.GetName())
}

// useCustomConfig 使用自定义配置
func (s *MailboxTestService) useCustomConfig() {
	// TODO: 新的统一Worker不再支持动态配置，配置应在启动时指定
	log.SysLogger.Infof("[%s] 使用自定义游戏场景配置（配置在启动时已指定）", s.GetName())
}

// scheduleTests 调度测试任务
func (s *MailboxTestService) scheduleTests() {
	// 测试1: 基础优先级测试 - 1秒后执行
	s.AfterFunc(time.Second*1, "基础优先级测试", func(timer *timingwheel.Timer, args ...interface{}) error {
		s.testBasicPriority()
		return nil
	})

	// 测试2: 混合优先级并发测试 - 3秒后执行
	s.AfterFunc(time.Second*3, "混合优先级并发测试", func(timer *timingwheel.Timer, args ...interface{}) error {
		s.testMixedPriorityConcurrent()
		return nil
	})

	// 测试3: 优先级顺序验证测试 - 6秒后执行
	s.AfterFunc(time.Second*6, "优先级顺序验证测试", func(timer *timingwheel.Timer, args ...interface{}) error {
		s.testPriorityOrder()
		return nil
	})

	// 测试4: 性能压测 - 9秒后执行
	s.AfterFunc(time.Second*9, "性能压测", func(timer *timingwheel.Timer, args ...interface{}) error {
		s.testPerformance()
		return nil
	})

	// 测试5: 调度策略验证 - 15秒后执行
	s.AfterFunc(time.Second*15, "调度策略验证", func(timer *timingwheel.Timer, args ...interface{}) error {
		s.testSchedulingStrategy()
		return nil
	})

	// 统计报告 - 20秒后输出
	s.AfterFunc(time.Second*20, "统计报告", func(timer *timingwheel.Timer, args ...interface{}) error {
		s.printStatistics()
		return nil
	})
}

// testBasicPriority 测试基础优先级功能
func (s *MailboxTestService) testBasicPriority() {
	ctx := context.Background()
	log.SysLogger.Infof("========== 测试1: 基础优先级测试 开始 ==========")

	// 发送不同优先级的消息
	s.PostSystemPriorityMessage(ctx, "系统消息测试")
	s.PostUrgentMessage(ctx, "紧急消息测试")
	s.PostHighPriorityMessage(ctx, "高优先级消息测试")
	s.PostNormalMessage(ctx, "普通消息测试")
	s.PostLowPriorityMessage(ctx, "低优先级消息测试")
	s.PostBatchMessage(ctx, "批量消息测试")
	s.PostBackgroundMessage(ctx, "后台消息测试")

	log.SysLogger.Infof("========== 测试1: 基础优先级测试 完成 ==========")
}

// testMixedPriorityConcurrent 测试混合优先级并发
func (s *MailboxTestService) testMixedPriorityConcurrent() {
	ctx := context.Background()
	log.SysLogger.Infof("========== 测试2: 混合优先级并发测试 开始 ==========")

	var wg sync.WaitGroup
	// 每种优先级发送10条消息
	messageCount := 10

	// 并发发送各种优先级的消息
	priorities := []def.Priority{
		def.PrioritySys,
		def.PriorityUrgent,
		def.PriorityHigh,
		def.PriorityNormal,
		def.PriorityLow,
		def.PriorityBatch,
		def.PriorityBackground,
	}

	for _, priority := range priorities {
		wg.Add(1)
		go func(p def.Priority) {
			defer wg.Done()
			for i := 0; i < messageCount; i++ {
				s.postMessageWithPriority(ctx, p, fmt.Sprintf("并发测试消息-%d", i))
				// 稍微延迟，模拟真实场景
				time.Sleep(time.Millisecond * 5)
			}
		}(priority)
	}

	wg.Wait()
	log.SysLogger.Infof("========== 测试2: 混合优先级并发测试 完成 ==========")
}

// testPriorityOrder 测试优先级顺序
func (s *MailboxTestService) testPriorityOrder() {
	ctx := context.Background()
	log.SysLogger.Infof("========== 测试3: 优先级顺序验证测试 开始 ==========")

	// 先发送低优先级消息
	for i := 0; i < 20; i++ {
		s.PostBackgroundMessage(ctx, fmt.Sprintf("背景消息-%d", i))
		s.PostLowPriorityMessage(ctx, fmt.Sprintf("低优先级消息-%d", i))
	}

	// 稍微延迟后发送高优先级消息，验证高优先级能否插队
	time.Sleep(time.Millisecond * 100)

	for i := 0; i < 5; i++ {
		s.PostUrgentMessage(ctx, fmt.Sprintf("紧急消息插队-%d", i))
		s.PostSystemPriorityMessage(ctx, fmt.Sprintf("系统消息插队-%d", i))
	}

	log.SysLogger.Infof("========== 测试3: 优先级顺序验证测试 完成 ==========")
}

// testPerformance 性能压测
func (s *MailboxTestService) testPerformance() {
	ctx := context.Background()
	log.SysLogger.Infof("========== 测试4: 性能压测 开始 ==========")

	startTime := time.Now()
	totalMessages := 10000
	var wg sync.WaitGroup

	// 多协程并发发送消息
	goroutineCount := 10
	messagesPerGoroutine := totalMessages / goroutineCount

	for i := 0; i < goroutineCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < messagesPerGoroutine; j++ {
				// 模拟不同优先级的消息分布
				priority := s.getRandomPriority(j)
				s.postMessageWithPriority(ctx, priority, fmt.Sprintf("压测消息-G%d-M%d", id, j))
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(startTime)

	log.SysLogger.Infof("========== 测试4: 性能压测 完成 ==========")
	log.SysLogger.Infof("发送 %d 条消息耗时: %v", totalMessages, duration)
	log.SysLogger.Infof("平均每秒处理: %.2f 条消息", float64(totalMessages)/duration.Seconds())
}

// testSchedulingStrategy 测试调度策略
func (s *MailboxTestService) testSchedulingStrategy() {
	ctx := context.Background()
	log.SysLogger.Infof("========== 测试5: 调度策略验证 开始 ==========")

	// 持续发送低优先级消息，同时间隔发送高优先级消息
	// 验证调度策略是否能正确处理

	done := make(chan bool)
	var wg sync.WaitGroup

	// 协程1: 持续发送大量低优先级消息
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			select {
			case <-done:
				return
			default:
				s.PostLowPriorityMessage(ctx, fmt.Sprintf("低优先级持续消息-%d", i))
				s.PostBackgroundMessage(ctx, fmt.Sprintf("后台持续消息-%d", i))
				time.Sleep(time.Millisecond * 10)
			}
		}
	}()

	// 协程2: 间隔发送高优先级消息
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			select {
			case <-done:
				return
			default:
				s.PostUrgentMessage(ctx, fmt.Sprintf("紧急间隔消息-%d", i))
				s.PostHighPriorityMessage(ctx, fmt.Sprintf("高优先级间隔消息-%d", i))
				time.Sleep(time.Millisecond * 50)
			}
		}
	}()

	// 等待测试完成
	time.Sleep(time.Second * 3)
	close(done)
	wg.Wait()

	log.SysLogger.Infof("========== 测试5: 调度策略验证 完成 ==========")
}

// getRandomPriority 获取随机优先级（用于性能测试）
func (s *MailboxTestService) getRandomPriority(seed int) def.Priority {
	// 模拟真实场景的优先级分布：
	// 系统消息 5%、紧急 10%、高 15%、普通 40%、低 15%、批量 10%、后台 5%
	mod := seed % 100
	switch {
	case mod < 5:
		return def.PrioritySys
	case mod < 15:
		return def.PriorityUrgent
	case mod < 30:
		return def.PriorityHigh
	case mod < 70:
		return def.PriorityNormal
	case mod < 85:
		return def.PriorityLow
	case mod < 95:
		return def.PriorityBatch
	default:
		return def.PriorityBackground
	}
}

// printStatistics 打印统计信息
func (s *MailboxTestService) printStatistics() {
	s.stats.duration = time.Since(s.stats.startTime)

	log.SysLogger.Infof("==================== Mailbox 测试统计报告 ====================")
	log.SysLogger.Infof("测试持续时间: %v", s.stats.duration)
	log.SysLogger.Infof("")

	log.SysLogger.Infof("各优先级消息处理统计:")
	s.printPriorityStats("系统消息", s.stats.sysCount.Load(), s.stats.sysProcessTime.Load())
	s.printPriorityStats("紧急消息", s.stats.urgentCount.Load(), s.stats.urgentProcessTime.Load())
	s.printPriorityStats("高优先级", s.stats.highCount.Load(), s.stats.highProcessTime.Load())
	s.printPriorityStats("普通消息", s.stats.normalCount.Load(), s.stats.normalProcessTime.Load())
	s.printPriorityStats("低优先级", s.stats.lowCount.Load(), s.stats.lowProcessTime.Load())
	s.printPriorityStats("批量消息", s.stats.batchCount.Load(), s.stats.batchProcessTime.Load())
	s.printPriorityStats("后台消息", s.stats.backgroundCount.Load(), s.stats.backgroundProcessTime.Load())

	totalCount := s.stats.sysCount.Load() + s.stats.urgentCount.Load() +
		s.stats.highCount.Load() + s.stats.normalCount.Load() +
		s.stats.lowCount.Load() + s.stats.batchCount.Load() +
		s.stats.backgroundCount.Load()

	totalProcessTime := s.stats.sysProcessTime.Load() + s.stats.urgentProcessTime.Load() +
		s.stats.highProcessTime.Load() + s.stats.normalProcessTime.Load() +
		s.stats.lowProcessTime.Load() + s.stats.batchProcessTime.Load() +
		s.stats.backgroundProcessTime.Load()

	log.SysLogger.Infof("")
	log.SysLogger.Infof("总计:")
	log.SysLogger.Infof("  总消息数: %d", totalCount)
	log.SysLogger.Infof("  总处理时间: %v", time.Duration(totalProcessTime))
	if totalCount > 0 {
		log.SysLogger.Infof("  平均处理时间: %v", time.Duration(totalProcessTime/totalCount))
		log.SysLogger.Infof("  吞吐量: %.2f 条/秒", float64(totalCount)/s.stats.duration.Seconds())
	}
	log.SysLogger.Infof("==============================================================")
}

// printPriorityStats 打印单个优先级的统计
func (s *MailboxTestService) printPriorityStats(name string, count int64, totalTime int64) {
	avgTime := int64(0)
	if count > 0 {
		avgTime = totalTime / count
	}
	log.SysLogger.Infof("  %s: 数量=%d, 总耗时=%v, 平均=%v",
		name, count, time.Duration(totalTime), time.Duration(avgTime))
}

// postMessageWithPriority 发送指定优先级的消息
func (s *MailboxTestService) postMessageWithPriority(ctx context.Context, priority def.Priority, message string) {
	switch priority {
	case def.PrioritySys:
		s.PostSystemPriorityMessage(ctx, message)
	case def.PriorityUrgent:
		s.PostUrgentMessage(ctx, message)
	case def.PriorityHigh:
		s.PostHighPriorityMessage(ctx, message)
	case def.PriorityNormal:
		s.PostNormalMessage(ctx, message)
	case def.PriorityLow:
		s.PostLowPriorityMessage(ctx, message)
	case def.PriorityBatch:
		s.PostBatchMessage(ctx, message)
	case def.PriorityBackground:
		s.PostBackgroundMessage(ctx, message)
	}
}

// ==================== 各优先级消息处理方法 ====================

// PostSystemPriorityMessage 系统优先级消息
func (s *MailboxTestService) PostSystemPriorityMessage(ctx context.Context, message string) {
	evt := event.NewEvent()
	evt.Type = EventTypeSystem
	evt.Data = message
	evt.Priority = def.PrioritySys

	s.GetMailbox().PostMessage(ctx, evt)
}

func (s *MailboxTestService) HandleSystemMessage(message string) {
	startTime := time.Now()
	s.stats.sysCount.Add(1)

	s.GetLogger().Debugf("[系统消息] %s", message)

	// 模拟处理耗时
	time.Sleep(time.Microsecond * 100)

	s.stats.sysProcessTime.Add(int64(time.Since(startTime)))
}

// PostUrgentMessage 紧急消息
func (s *MailboxTestService) PostUrgentMessage(ctx context.Context, message string) {
	evt := event.NewEvent()
	evt.Type = EventTypeUrgent
	evt.Data = message
	evt.Priority = def.PriorityUrgent

	s.GetMailbox().PostMessage(ctx, evt)
}

func (s *MailboxTestService) HandleUrgentMessage(message string) {
	startTime := time.Now()
	s.stats.urgentCount.Add(1)

	s.GetLogger().Debugf("[紧急消息] %s", message)

	time.Sleep(time.Microsecond * 150)

	s.stats.urgentProcessTime.Add(int64(time.Since(startTime)))
}

// PostHighPriorityMessage 高优先级消息
func (s *MailboxTestService) PostHighPriorityMessage(ctx context.Context, message string) {
	evt := event.NewEvent()
	evt.Type = EventTypeHigh
	evt.Data = message
	evt.Priority = def.PriorityHigh

	s.GetMailbox().PostMessage(ctx, evt)
}

func (s *MailboxTestService) HandleHighPriorityMessage(message string) {
	startTime := time.Now()
	s.stats.highCount.Add(1)

	s.GetLogger().Debugf("[高优先级] %s", message)

	time.Sleep(time.Microsecond * 200)

	s.stats.highProcessTime.Add(int64(time.Since(startTime)))
}

// PostNormalMessage 普通消息
func (s *MailboxTestService) PostNormalMessage(ctx context.Context, message string) {
	evt := event.NewEvent()
	evt.Type = EventTypeNormal
	evt.Data = message
	evt.Priority = def.PriorityNormal

	s.GetMailbox().PostMessage(ctx, evt)
}

func (s *MailboxTestService) HandleNormalMessage(message string) {
	startTime := time.Now()
	s.stats.normalCount.Add(1)

	s.GetLogger().Debugf("[普通消息] %s", message)

	time.Sleep(time.Microsecond * 300)

	s.stats.normalProcessTime.Add(int64(time.Since(startTime)))
}

// PostLowPriorityMessage 低优先级消息
func (s *MailboxTestService) PostLowPriorityMessage(ctx context.Context, message string) {
	evt := event.NewEvent()
	evt.Type = EventTypeLow
	evt.Data = message
	evt.Priority = def.PriorityLow

	s.GetMailbox().PostMessage(ctx, evt)
}

func (s *MailboxTestService) HandleLowPriorityMessage(message string) {
	startTime := time.Now()
	s.stats.lowCount.Add(1)

	s.GetLogger().Debugf("[低优先级] %s", message)

	time.Sleep(time.Microsecond * 400)

	s.stats.lowProcessTime.Add(int64(time.Since(startTime)))
}

// PostBatchMessage 批量消息
func (s *MailboxTestService) PostBatchMessage(ctx context.Context, message string) {
	evt := event.NewEvent()
	evt.Type = EventTypeBatch
	evt.Data = message
	evt.Priority = def.PriorityBatch

	s.GetMailbox().PostMessage(ctx, evt)
}

func (s *MailboxTestService) HandleBatchMessage(message string) {
	startTime := time.Now()
	s.stats.batchCount.Add(1)

	s.GetLogger().Debugf("[批量消息] %s", message)

	time.Sleep(time.Microsecond * 500)

	s.stats.batchProcessTime.Add(int64(time.Since(startTime)))
}

// PostBackgroundMessage 后台消息
func (s *MailboxTestService) PostBackgroundMessage(ctx context.Context, message string) {
	evt := event.NewEvent()
	evt.Type = EventTypeBackground
	evt.Data = message
	evt.Priority = def.PriorityBackground

	s.GetMailbox().PostMessage(ctx, evt)
}

func (s *MailboxTestService) HandleBackgroundMessage(message string) {
	startTime := time.Now()
	s.stats.backgroundCount.Add(1)

	s.GetLogger().Debugf("[后台消息] %s", message)

	time.Sleep(time.Microsecond * 600)

	s.stats.backgroundProcessTime.Add(int64(time.Since(startTime)))
}

func (s *MailboxTestService) OnStart() error {
	return nil
}

func (s *MailboxTestService) OnRelease() {
	log.SysLogger.Infof("[%s] 服务释放", s.GetName())
}

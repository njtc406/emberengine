# TimingWheel - 高性能时间轮定时器

## 概述

TimingWheel 是一个基于时间轮算法的高性能定时器库，专为多线程Actor模型设计。它提供了分层时间轮、并发安全的调度器、同步/异步定时任务和测试环境时间偏移能力。

## 特性

- ✅ **高性能时间轮算法** - O(1)时间复杂度的定时器插入和删除
- ✅ **并发安全** - 完整的并发保护机制
- ✅ **安全生命周期** - 当前版本不复用Timer对象，避免Timer已投递但对象被重置复用导致的ABA问题
- ✅ **多种定时器类型** - 支持一次性定时器、循环定时器、Cron表达式定时器
- ✅ **异步执行支持** - 支持在独立goroutine中执行任务
- ✅ **时间偏移支持** - 测试/开发环境支持时间偏移调整,所有定时器自动重算(生产环境禁止调用)

## 架构设计

```
┌─────────────────────────────────────────────────────────┐
│                   TaskScheduler                          │
│  (服务级别的调度器，管理Timer生命周期)                      │
└─────────────────┬───────────────────────────────────────┘
                  │
                  ▼
┌─────────────────────────────────────────────────────────┐
│                   TimingWheel                            │
│  (底层时间轮，基于开源实现，O(1)插入/删除)                  │
└─────────────────┬───────────────────────────────────────┘
                  │
                  ▼
┌─────────────────────────────────────────────────────────┐
│                   Timer                                  │
│  (每次创建独立对象；当前不使用对象池，优先生命周期安全)       │
└─────────────────────────────────────────────────────────┘
```

## 使用方式

### 1. 初始化全局时间轮

```go
import (
    "time"
    "github.com/njtc406/emberengine/engine/pkg/log"
    "github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
)

// 创建日志器（可选，传 nil 会使用默认日志器）
logger, _ := log.NewDefaultLogger(nil)

// 启动全局时间轮
// interval: 时间轮的tick间隔
// wheelSize: 时间轮的槽位数量
// logger: 日志器
timingwheel.Start(time.Millisecond, 100, logger)
defer timingwheel.Stop()
```

### 2. 创建调度器

每个服务应该创建自己的JobScheduler：

```go
// jobName: 调度器名称（用于日志）
// chanSize: 回调通道大小
// bucketSize: Timer分片数量
// tw: 时间轮实例（由节点注入）
// logger: 日志器
scheduler := timingwheel.NewJobScheduler(
    "myService",
    1000, 
    10, 
    timingwheel.GetTimingWheel(),
    log.NewLoggerX(logger, log.Fields{"pkg": "myService"}),
)
defer scheduler.Stop()
```

### 3. 注册定时器

#### 一次性定时器

```go
timerId, err := scheduler.AfterFunc(time.Second*5, "myTimer",
    func(ctx context.Context, t *timingwheel.Timer, args ...interface{}) error {
        fmt.Println("Timer fired!")
        return nil
    }, "arg1", "arg2")
```

#### 循环定时器

```go
// 每5秒执行一次
timerId, err := scheduler.TickerFunc(time.Second*5, "tickerTimer",
    func(ctx context.Context, t *timingwheel.Timer, args ...interface{}) error {
        fmt.Println("Tick!")
        return nil
    })
```

#### Cron定时器

```go
// 每分钟执行一次
timerId, err := scheduler.CronFunc("0 */1 * * * *", "cronTimer",
    func(ctx context.Context, t *timingwheel.Timer, args ...interface{}) error {
        fmt.Println("Cron fired!")
        return nil
    })

// 或使用 @every 语法
timerId, err := scheduler.CronFunc("@every 5s", "cronTimer",
    func(ctx context.Context, t *timingwheel.Timer, args ...interface{}) error {
        fmt.Println("Every 5 seconds!")
        return nil
    })
```

#### 异步定时器

```go
// 在独立goroutine中执行
timerId, err := scheduler.AfterAsyncFunc(time.Second*5, "asyncTimer",
    func(ctx context.Context, t *timingwheel.Timer, args ...interface{}) error {
        // 在独立goroutine中执行
        fmt.Println("Async execution!")
        return nil
    }, "arg1")
```

### 4. 取消定时器

```go
// 通过ID取消
scheduler.CancelTimer(timerId)
```

### 5. 监听回调（Service集成）

在Service中，定时器回调会自动投递到mailbox：

```go
// Service会自动处理timer回调
func (s *Service) startListenCallback() {
    for {
        select {
        case t, ok := <-s.ITimerScheduler.GetTimerCbChannel():
            if !ok {
                return
            }
            // 执行timer回调
            if err := t.Do(context.Background()); err != nil {
                s.logger.Errorf("timer callback error: %v", err)
            }
        }
    }
}
```

## Timer复用与ABA问题说明

当前生产代码**不使用 `sync.Pool` 复用 `Timer` 对象**。这是有意设计：Timer 触发后可能已经被投递到 callback channel 中等待业务消费，如果此时对象被回收并复用给另一个业务，旧 channel 中的指针后续被取出时就可能执行成新业务的 Timer。

因此当前策略是：

- 每次创建新的 `Timer` 对象；
- 依靠 `cancel` 状态、scheduler 分片表、bucket 移除和 `addOrRun` active 检查保证生命周期安全；
- 不为了减少少量分配而引入对象复用风险。

如果未来确实需要恢复 `Timer` 对象池，必须同时引入 generation/triggerGeneration 形式的ABA防护。可参考仓库记忆中的 `timingwheel-aba-timer-reuse.md` 方案。

### 未来对象池方案备忘

### 问题场景

在多线程Actor模式下，可能出现以下情况：

1. 服务A注册Timer，触发后投递到A的mailbox
2. 服务A负载高，Timer在mailbox中排队
3. Timer被取消并回收到对象池
4. 服务B注册新Timer，复用了同一个Timer对象
5. 服务A从mailbox取出Timer执行 → **错误执行了B的Timer！**

如果未来恢复对象池，可使用**触发快照版本号**机制，在时间轮弹出Timer时冻结版本号：

```go
type Timer struct {
    generation        atomic.Uint64 // 每次初始化/复用递增
    triggerGeneration atomic.Uint64 // 触发投递前冻结，用于执行校验
    // ... 其他字段
}

func (tw *TimingWheel) runTimer(t *Timer, runLoop bool) {
    t.triggerGeneration.Store(t.generation.Load())
    // ... 投递到channel或执行异步任务
}

func (t *Timer) Do(ctx context.Context) error {
    if t.triggerGeneration.Load() != t.generation.Load() {
        // 版本号不匹配，Timer已被回收并复用，丢弃
        return nil
    }
    // ... 执行任务
}

func (t *Timer) resetForReuse() {
    t.generation.Add(1)
    t.triggerGeneration.Store(0)
    // ... 重置字段
}
```

**时序保证：**
```
T1: TimingWheel检测到Timer到期
T2: runTimer()被冻结 triggerGeneration = generation
T3: c <- t 投递到channel，或启动异步任务
T4: 如果Timer被回收并复用，generation递增
T5: Service从channel接收旧timer指针
T6: Do()内部验证 triggerGeneration == generation
T7: 验证通过则执行，否则丢弃
```

**优势：**
- ✅ 版本号在投递前冻结，降低对象复用导致的ABA风险
- ✅ 验证逻辑封装在Timer.Do()内部，使用方无需关心
- ✅ 零额外对象分配
- ⚠️ 仍需配合严格的回收时机管理，不能在Timer仍可能位于callback channel或异步goroutine中时直接复用

## 性能特性

### 时间复杂度

| 操作 | 时间复杂度 | 说明 |
|------|-----------|------|
| 添加定时器 | O(1) | 直接插入时间轮槽位 |
| 删除定时器 | O(1) | 直接从槽位删除 |
| 触发定时器 | O(n) | n为当前槽位中的定时器数量 |

### 内存优化

- **生命周期优先**：当前不复用Timer对象，避免已投递Timer被重置复用
- **分片存储**：Timer按ID分片存储，减少锁竞争

### 并发安全

- ✅ Timer字段分为可变（atomic）和不可变（初始化后只读）
- ✅ Stop/Cancel通过atomic状态、scheduler分片表和bucket移除协同处理
- ✅ 同步回调通道只读暴露，内部投递与关闭有同步保护
- ✅ TimingWheel调整时间偏移时有调整锁和pending队列保护

## 优缺点分析

### 优点

1. **高性能**
   - O(1)的定时器添加和删除
    - 分片存储减少调度器锁竞争
    - 长延迟Timer通过分层overflow wheel覆盖，层级增长慢

2. **易用性**
   - 支持多种定时器类型（一次性、循环、Cron）
   - 与Service/Mailbox无缝集成
   - 简洁的API设计

3. **可靠性**
   - 完整的并发保护
    - 不复用Timer对象，避免对象池ABA风险
   - 优雅的关闭机制

4. **灵活性**
   - 支持异步执行
   - 支持Cron表达式
   - 可自定义回调参数

### 缺点

1. **精度限制**
   - 精度取决于时间轮的tick间隔
   - 不适合需要纳秒级精度的场景

2. **内存占用**
   - 时间轮需要预分配槽位数组
    - 当前Timer不使用对象池，换取更简单可靠的生命周期语义

3. **单节点限制**
   - 全局只有一个TimingWheel实例
   - 不支持分布式定时器

4. **Cron精度**
   - Cron定时器精度只到秒级
   - 不支持毫秒级Cron表达式

## 最佳实践

### 1. 合理设置时间轮参数

```go
// 根据业务需求调整
// 短周期高频场景：小interval，大wheelSize
timingwheel.Start(time.Millisecond*10, 100)

// 长周期低频场景：大interval，小wheelSize
timingwheel.Start(time.Second, 60)
```

### 2. 及时取消不需要的定时器

```go
timerId, _ := scheduler.AfterFunc(...)
defer scheduler.CancelTimer(timerId)
```

### 3. 异步定时器的使用场景

```go
// 同步定时器：回调在Service的Worker线程中顺序执行
// 适用于需要保证执行顺序的业务逻辑
scheduler.AfterFunc(time.Second, "syncTimer", func(ctx context.Context, t *Timer, args ...interface{}) error {
    // 在Service的mailbox线程中执行
    // 保证与其他消息的执行顺序
    db.Update(...)  // 可以安全执行耗时操作，不会阻塞时间轮
    return nil
})

// 异步定时器：回调在独立goroutine中执行
// 适用于不需要保证执行顺序，可以并发执行的任务
scheduler.AfterAsyncFunc(time.Second, "asyncTimer", func(ctx context.Context, t *Timer, args ...interface{}) error {
    // 在独立goroutine中执行
    // 不保证与Service其他消息的执行顺序
    heavyComputation()  // 可以并发执行
    return nil
})
```

**说明：**
- 时间轮弹出Timer后，通过channel异步投递到Service，Timer的执行与时间轮完全解耦
- 同步定时器在Service的Worker线程中执行，保证消息处理顺序
- 异步定时器在独立goroutine中执行，不保证顺序但可以并发
- 选择同步还是异步取决于业务逻辑是否需要保证执行顺序

### 4. 合理设置回调通道大小

```go
// 根据并发定时器数量设置
scheduler := timingwheel.NewJobScheduler(
    "myService",
    10000,  // 回调通道大小
    10,     // 分片数量
    timingwheel.GetTimingWheel(),
    logger,
)
```

## 注意事项

1. **全局初始化**：必须先调用`timingwheel.Start()`启动全局时间轮
2. **优雅关闭**：程序退出前调用`timingwheel.Stop()`和`scheduler.Stop()`
3. **Timer生命周期**：当前不复用Timer对象；如未来恢复对象池，必须同时加入ABA版本号校验
4. **回调执行上下文**：回调在Service的Worker线程中执行,可安全访问Service状态
5. **Cron表达式**：使用标准Cron格式（秒 分 时 日 月 周）或`@every`语法

## 时间偏移功能(仅开发环境)

### 使用场景

在开发环境中,可能需要快速调试某个时间点的功能,此时可以使用时间偏移功能。**注意:这个功能只应该在开发/测试环境使用,生产环境不应该调整时间偏移。**

### 使用方法

```go
import (
    "time"
    "github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
)

// 设置时间偏移（向前跳10小时）
timingwheel.SetTimeOffset(10 * time.Hour)

// 也可以向后偏移
timingwheel.SetTimeOffset(-30 * time.Minute)

// 恢复正常时间
timingwheel.SetTimeOffset(0)
```

### 工作原理

当调用 `SetTimeOffset` 时,时间轮会执行以下操作:

1. **设置调整标志** - 阻止新的timer直接插入，改为放入缓冲队列
2. **收集所有活跃的定时器** - 递归遍历所有层级的bucket,收集正在等待的Timer
3. **清空所有bucket** - 移除所有Timer,准备重新插入
4. **调整currentTime** - 更新时间轮的当前时间
5. **处理跨越执行点的Timer** - 对于正向偏移，检查并执行已跨越执行时间的任务
6. **重新插入Timer** - 将调整后的Timer重新插入到正确的bucket
7. **处理缓冲队列** - 处理调整期间累积的pending timers
8. **递归调整overflow wheel** - 如果有多层时间轮,递归调整

```go
// 内部实现示例
func (tw *TimingWheel) SetTimeOffset(offset time.Duration) {
    tw.adjustMu.Lock()
    defer tw.adjustMu.Unlock()
    
    // 设置调整标志
    tw.adjusting.Store(true)
    defer func() {
        tw.adjusting.Store(false)
        close(tw.adjustDone)
        tw.adjustDone = make(chan struct{})
    }()
    
    // 计算偏移差值
    oldOffsetNs := tw.timeOffsetNs.Load()
    newOffsetNs := int64(offset)
    offsetDeltaNs := newOffsetNs - oldOffsetNs
    
    // 原子更新offset
    tw.timeOffsetNs.Store(newOffsetNs)
    offsetDeltaMs := offsetDeltaNs / int64(time.Millisecond)
    
    // 1. 收集所有活跃的 timer
    allTimers := tw.collectAllTimers()
    
    // 2. 清空所有 bucket 并调整 currentTime
    tw.clearAllBucketsRecursive()
    newCurrentTime := oldCurrentTime + offsetDeltaMs
    atomic.StoreInt64(&tw.currentTime, truncate(newCurrentTime, tw.tick))
    
    // 3. 处理跨越执行点的Timer（正向偏移时）
    if offsetDeltaMs > 0 {
        for _, t := range allTimers {
            if newCurrentTime >= t.GetExpiration() {
                tw.runTimer(t, false) // 执行
                // 周期性任务重新计算下次执行时间
            }
        }
    }
    
    // 4. 重新插入未执行的Timer
    for _, t := range allTimers {
        tw.addOrRunDirect(t)
    }
    
    // 5. 处理缓冲队列
    tw.processPendingTimers()
}
```

### 性能考虑

**警告:** 时间偏移调整是一个**昂贵的操作**,会:
- 设置调整标志，阻塞新的定时器操作
- 遍历所有bucket收集Timer (O(n))
- 清空所有bucket (O(n))
- 检查并执行跨越执行点的Timer
- 重新插入所有Timer (O(n))
- 处理缓冲队列中的pending timers

因此:
- ✅ **适用场景**: 开发/测试环境,快速调试时间相关功能
- ❌ **不适用场景**: 生产环境,高频调用
- ⚠️ **建议**: 只在必要时使用,避免在运行时频繁调整

### 示例场景

#### 场景1: 测试每日0点的任务

```go
// 创建一个每天0点执行的任务
timerId, _ := scheduler.CronFunc("0 0 0 * * *", "daily_task", 
    func(ctx context.Context, t *Timer, args ...interface{}) error {
        fmt.Println("执行每日任务")
        return nil
    })

// 不想等到明天0点,直接设置时间偏移到明天0点
tomorrow := time.Now().Truncate(24 * time.Hour).Add(24 * time.Hour)
offset := tomorrow.Sub(time.Now())

// 设置时间偏移
timingwheel.SetTimeOffset(offset)

// 等待任务执行
time.Sleep(2 * time.Second)

// 恢复正常时间
timingwheel.SetTimeOffset(0)
```

#### 场景2: 测试时间回退

```go
// 测试时间回退后定时器是否正常
timerId, _ := scheduler.AfterFunc(5*time.Second, "test", 
    func(ctx context.Context, t *Timer, args ...interface{}) error {
        fmt.Println("5秒后执行")
        return nil
    })

// 回退10秒
timingwheel.SetTimeOffset(-10 * time.Second)

// 现在定时器还需要等待更长时间才会执行
time.Sleep(3 * time.Second)
// 任务不应该执行

// 恢复正常时间后继续等待
timingwheel.SetTimeOffset(0)
time.Sleep(5 * time.Second)
// 现在任务应该执行了
```

### 测试用例

完整的测试用例请参考 `time_adjust_test.go`:
- `TestTimeAdjustment` - 基础时间偏移测试
- `TestTimeAdjustmentWithMultipleTimers` - 多定时器偏移测试
- `TestTimeAdjustmentWithTickerTimer` - 循环定时器偏移测试
- `TestTimeAdjustmentBackward` - 时间回退测试

## ITimerScheduler 接口

```go
type ITimerScheduler interface {
    // 一次性定时器
    AfterFunc(d time.Duration, name string, f TimerCallback, args ...interface{}) (uint64, error)
    AfterAsyncFunc(d time.Duration, name string, f TimerCallback, args ...interface{}) (uint64, error)
    
    // 循环定时器
    TickerFunc(d time.Duration, name string, f TimerCallback, args ...interface{}) (uint64, error)
    TickerAsyncFunc(d time.Duration, name string, f TimerCallback, args ...interface{}) (uint64, error)
    
    // Cron定时器
    CronFunc(spec string, name string, f TimerCallback, args ...interface{}) (uint64, error)
    CronAsyncFunc(spec string, name string, f TimerCallback, args ...interface{}) (uint64, error)
    
    // 取消定时器
    CancelTimer(taskId uint64)
    
    // 停止调度器
    Stop()
    
    // 获取回调通道
    GetTimerCbChannel() <-chan ITimer
}

// 回调函数类型
type TimerCallback func(ctx context.Context, timer *Timer, args ...interface{}) error
```

## 示例

完整示例请参考：
- `example_task_scheduler_test.go` - 基础使用示例
- `concurrent_test.go` - 并发安全测试
- `engine/pkg/core/service.go` - Service集成示例

## 相关文档

- [时间轮算法原理](https://en.wikipedia.org/wiki/Timing_wheel)
- [Cron表达式语法](https://pkg.go.dev/github.com/robfig/cron/v3)

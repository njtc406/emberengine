# TimingWheel - 高性能时间轮定时器

## 概述

TimingWheel 是一个基于时间轮算法的高性能定时器库，专为多线程Actor模型设计。它提供了对象池复用、并发安全和ABA问题防护等特性。

## 特性

- ✅ **高性能时间轮算法** - O(1)时间复杂度的定时器插入和删除
- ✅ **对象池复用** - Timer对象复用,减少GC压力
- ✅ **并发安全** - 完整的并发保护机制
- ✅ **ABA问题防护** - 通过generation版本号机制防止对象复用导致的错误执行
- ✅ **多种定时器类型** - 支持一次性定时器、循环定时器、Cron表达式定时器
- ✅ **异步执行支持** - 支持在独立goroutine中执行任务
- ✅ **时间调整支持** - 开发环境支持时间调整,所有定时器自动重算(仅开发环境)

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
│                   Timer Pool                             │
│  (对象池，复用Timer对象，带generation版本号)               │
└─────────────────────────────────────────────────────────┘
```

## 使用方式

### 1. 初始化全局时间轮

```go
import "github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"

// 启动全局时间轮
// interval: 时间轮的tick间隔
// wheelSize: 时间轮的槽位数量
timingwheel.Start(time.Millisecond, 20)
defer timingwheel.Stop()
```

### 2. 创建调度器

每个服务应该创建自己的TaskScheduler：

```go
// chanSize: 回调通道大小
// bucketSize: Timer分片数量
scheduler := timingwheel.NewTaskScheduler(1000, 10)
defer scheduler.Stop()
```

### 3. 注册定时器

#### 一次性定时器

```go
timerId, err := scheduler.AfterFunc(time.Second*5, "myTimer",
    func(t *timingwheel.Timer, args ...interface{}) {
        fmt.Println("Timer fired!")
    }, "arg1", "arg2")
```

#### 循环定时器

```go
// 每5秒执行一次
timer := scheduler.TickerFunc(time.Second*5, "tickerTimer",
    func(t *timingwheel.Timer, args ...interface{}) {
        fmt.Println("Tick!")
    })
```

#### Cron定时器

```go
// 每分钟执行一次
timer := scheduler.CronFunc("0 */1 * * * *", "cronTimer",
    func(t *timingwheel.Timer, args ...interface{}) {
        fmt.Println("Cron fired!")
    })

// 或使用 @every 语法
timer := scheduler.CronFunc("@every 5s", "cronTimer",
    func(t *timingwheel.Timer, args ...interface{}) {
        fmt.Println("Every 5 seconds!")
    })
```

#### 异步定时器

```go
// 在独立goroutine中执行
timer := scheduler.AfterAsyncFunc(time.Second*5, "asyncTimer",
    func(args ...interface{}) {
        // 在独立goroutine中执行
        fmt.Println("Async execution!")
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
            if err := s.pushTimerCallback(t); err != nil {
                s.logger.Errorf("submit timer callback error: %v", err)
            }
        }
    }
}
```

## ABA问题防护机制

### 问题场景

在多线程Actor模式下，可能出现以下情况：

1. 服务A注册Timer，触发后投递到A的mailbox
2. 服务A负载高，Timer在mailbox中排队
3. Timer被取消并回收到对象池
4. 服务B注册新Timer，复用了同一个Timer对象
5. 服务A从mailbox取出Timer执行 → **错误执行了B的Timer！**

### 解决方案

使用**snapGen快照版本号**机制，在时间轮弹出Timer时冻结版本号：

```go
// Timer结构体包含两个版本号
type Timer struct {
    generation atomic.Uint64  // 每次Reset递增
    snapGen    atomic.Uint64  // 在addOrRun中冻结，用于验证
    execWg     sync.WaitGroup // 等待执行完成（避免忙等待）
    // ... 其他字段
}

// TimingWheel在Timer到期时冻结版本号
func (tw *TimingWheel) addOrRun(t *Timer) {
    if !tw.add(t) {
        // 任务已经过期，立即执行
        // 在执行前冻结snapGen，防止ABA问题
        t.snapGen.Store(t.generation.Load())
        // ... 投递到channel
    }
}

// Timer.Do()执行时验证版本号
func (t *Timer) Do() {
    t.execWg.Add(1)
    defer t.execWg.Done()
    
    // 对比snapGen和generation
    if t.snapGen.Load() != t.generation.Load() {
        // 版本号不匹配，Timer已被回收并复用，丢弃
        return
    }
    // ... 执行任务
}

// Reset时递增generation，使旧引用失效
func (t *Timer) Reset() {
    t.execWg.Wait()  // 高效等待执行完成（零CPU占用）
    t.generation.Add(1)
    // ... 重置字段
}
```

**时序保证：**
```
T1: TimingWheel检测到Timer到期
T2: addOrRun()被调用
T3: snapGen = generation （冻结版本号）✓
T4: c <- t (投递到channel)
--- 即使这里Timer被Stop()并回收，snapGen已经冻结 ---
T5: Service从channel接收timer
T6: 执行Timer.Do()
T7: Do()内部验证 snapGen == generation
T8: 验证通过则执行，否则直接返回
```

**优势：**
- ✅ 版本号在投递前冻结，避免并发竞态
- ✅ 验证逻辑封装在Timer.Do()内部，使用方无需关心
- ✅ 零额外对象分配
- ✅ 完全防止ABA问题

## 性能特性

### 时间复杂度

| 操作 | 时间复杂度 | 说明 |
|------|-----------|------|
| 添加定时器 | O(1) | 直接插入时间轮槽位 |
| 删除定时器 | O(1) | 直接从槽位删除 |
| 触发定时器 | O(n) | n为当前槽位中的定时器数量 |

### 内存优化

- **对象池复用**：Timer对象通过sync.Pool复用，减少GC压力
- **零忙等待**：使用sync.WaitGroup替代忙等待，Reset/Stop时零CPU占用
- **分片存储**：Timer按ID分片存储，减少锁竞争

### 并发安全

- ✅ Timer字段分为可变（atomic）和不可变（初始化后只读）
- ✅ executing标志防止重复执行
- ✅ Stop/Reset使用WaitGroup高效等待执行完成（零CPU占用）
- ✅ 全局TimingWheel有mutex保护

## 优缺点分析

### 优点

1. **高性能**
   - O(1)的定时器添加和删除
   - 对象池复用，减少GC压力
   - WaitGroup零CPU等待，高并发友好

2. **易用性**
   - 支持多种定时器类型（一次性、循环、Cron）
   - 与Service/Mailbox无缝集成
   - 简洁的API设计

3. **可靠性**
   - 完整的并发保护
   - ABA问题防护
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
   - 长时间定时器可能占用多个槽位

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
scheduler.AfterFunc(time.Second, "syncTimer", func(t *Timer, args ...interface{}) {
    // 在Service的mailbox线程中执行
    // 保证与其他消息的执行顺序
    db.Update(...)  // 可以安全执行耗时操作，不会阻塞时间轮
})

// 异步定时器：回调在独立goroutine中执行
// 适用于不需要保证执行顺序，可以并发执行的任务
scheduler.AfterAsyncFunc(time.Second, "asyncTimer", func(args ...interface{}) {
    // 在独立goroutine中执行
    // 不保证与Service其他消息的执行顺序
    heavyComputation()  // 可以并发执行
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
scheduler := NewTaskScheduler(
    10000,  // 回调通道大小
    10,     // 分片数量
)
```

## 注意事项

1. **全局初始化**：必须先调用`timingwheel.Start()`启动全局时间轮
2. **优雅关闭**：程序退出前调用`timingwheel.Stop()`和`scheduler.Stop()`
3. **版本号验证**：Timer.Do()内部自动验证版本号,防止ABA问题
4. **回调执行上下文**：回调在Service的Worker线程中执行,可安全访问Service状态
5. **Cron表达式**：使用标准Cron格式（秒 分 时 日 月 周）或`@every`语法

## 时间调整功能(仅开发环境)

### 使用场景

在开发环境中,可能需要快速调试某个时间点的功能,此时可以使用时间调整功能。**注意:这个功能只应该在开发/测试环境使用,生产环境不应该调整时间。**

### 使用方法

```go
import (
    "time"
    "github.com/njtc406/emberengine/engine/pkg/utils/timelib"
    "github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
)

// 1. 先调整 timelib 的时间偏移
offset := int64(10 * time.Hour) // 向前跳10小时
timelib.SetTimeOffset(offset)

// 2. 再调整时间轮中的所有定时器
timingwheel.AdjustTime(offset / int64(time.Millisecond))

// 或者使用 TaskScheduler 接口
scheduler.AdjustTime(offset / int64(time.Millisecond))
```

### 工作原理

当调用 `AdjustTime` 时,时间轮会执行以下操作:

1. **收集所有活跃的定时器** - 遍历所有bucket,收集正在等待的Timer
2. **清空所有bucket** - 移除所有Timer,准备重新插入
3. **调整currentTime** - 更新时间轮的当前时间
4. **调整Timer过期时间** - 对每个Timer的过期时间应用offset
5. **重新插入Timer** - 将调整后的Timer重新插入到正确的bucket
6. **递归调整overflow wheel** - 如果有多层时间轮,递归调整

```go
// 内部实现示例
func (tw *TimingWheel) AdjustTime(offsetMs int64) {
    tw.adjustMu.Lock()
    defer tw.adjustMu.Unlock()
    
    // 1. 收集所有活跃的 timer
    allTimers := tw.collectAllTimers()
    
    // 2. 清空所有 bucket
    tw.clearAllBuckets()
    
    // 3. 调整 currentTime
    oldCurrentTime := atomic.LoadInt64(&tw.currentTime)
    newCurrentTime := oldCurrentTime + offsetMs
    atomic.StoreInt64(&tw.currentTime, truncate(newCurrentTime, tw.tick))
    
    // 4. 调整所有 timer 的过期时间并重新插入
    for _, t := range allTimers {
        oldExpiration := t.GetExpiration()
        newExpiration := oldExpiration + offsetMs
        t.SetExpiration(newExpiration)
        tw.addOrRun(t) // 重新插入
    }
    
    // 5. 递归调整 overflow wheel
    if overflowWheel != nil {
        overflowWheel.adjustTimeInternal(offsetMs)
    }
}
```

### 性能考虑

**警告:** 时间调整是一个**昂贵的操作**,会:
- 遍历所有bucket收集Timer (O(n))
- 清空所有bucket (O(n))
- 重新插入所有Timer (O(n))
- 期间会加写锁,阻塞所有定时器操作

因此:
- ✅ **适用场景**: 开发/测试环境,快速调试时间相关功能
- ❌ **不适用场景**: 生产环境,高频调用
- ⚠️ **建议**: 只在必要时使用,避免在运行时频繁调整

### 示例场景

#### 场景1: 测试每日0点的任务

```go
// 创建一个每天0点执行的任务
timerId, _ := scheduler.CronFunc("0 0 0 * * *", "daily_task", 
    func(t *Timer, args ...interface{}) error {
        fmt.Println("执行每日任务")
        return nil
    })

// 不想等到明天0点,直接跳到明天0点
tomorrow := timelib.GetDayStartTime(timelib.Now().Add(24 * time.Hour))
offset := tomorrow.Sub(timelib.Now())

// 调整时间
timelib.SetTimeOffset(int64(offset))
timingwheel.AdjustTime(int64(offset / time.Millisecond))

// 等待任务执行
time.Sleep(2 * time.Second)

// 恢复时间
timelib.SetTimeOffset(0)
timingwheel.AdjustTime(-int64(offset / time.Millisecond))
```

#### 场景2: 测试时间回退

```go
// 测试时间回退后定时器是否正常
timerId, _ := scheduler.AfterFunc(5*time.Second, "test", 
    func(t *Timer, args ...interface{}) error {
        fmt.Println("5秒后执行")
        return nil
    })

// 回退10秒
offset := int64(-10 * time.Second)
timelib.SetTimeOffset(offset)
timingwheel.AdjustTime(offset / int64(time.Millisecond))

// 现在定时器还需要等待 5+10=15秒才会执行
time.Sleep(3 * time.Second)
// 任务不应该执行

time.Sleep(13 * time.Second)
// 现在任务应该执行了
```

### 测试用例

完整的测试用例请参考 `time_adjust_test.go`:
- `TestTimeAdjustment` - 基础时间调整测试
- `TestTimeAdjustmentWithMultipleTimers` - 多定时器调整测试
- `TestTimeAdjustmentWithTickerTimer` - 循环定时器调整测试
- `TestTimeAdjustmentBackward` - 时间回退测试

## 示例

完整示例请参考：
- `example_task_scheduler_test.go` - 基础使用示例
- `concurrent_test.go` - 并发安全测试
- `engine/pkg/core/service.go` - Service集成示例

## 相关文档

- [时间轮算法原理](https://en.wikipedia.org/wiki/Timing_wheel)
- [Cron表达式语法](https://pkg.go.dev/github.com/robfig/cron/v3)

# Worker 扩缩容策略待实现列表

## 当前已实现

### ✅ MaxLoadStrategy (最大负载策略)
- **文件**: `strategy.go`
- **实现状态**: 已完成
- **功能**: 基于队列长度和Worker空闲率进行扩缩容
- **配置参数**:
  - `IdleThreshold`: 空闲Worker比例阈值
  - `MaxLoadThreshold`: 单个Worker最大负载阈值

### ✅ CompositeStrategy (复合策略)
- **文件**: `strategy.go`
- **实现状态**: 已完成
- **功能**: 组合多个策略，支持any/all模式
- **配置参数**:
  - `Mode`: "any" 或 "all"
  - `Subs`: 子策略列表

---

## 待实现策略（按优先级排序）

### 🔥 高优先级（强烈推荐）

#### 1. CooldownStrategy (冷却期策略)
**优先级**: ⭐⭐⭐⭐⭐

**实现方式**: 装饰器模式，包装现有策略

**功能描述**:
- 防止扩缩容震荡
- 扩容后N分钟内不允许缩容
- 缩容后M分钟内不允许扩容
- 连续K次检测到需要扩缩容才执行

**配置参数**:
```yaml
ScaleUpCooldown: 5m        # 扩容冷却期
ScaleDownCooldown: 10m     # 缩容冷却期
MinConsecutiveCount: 3     # 连续检测次数阈值
```

**实现要点**:
```go
type CooldownWrapper struct {
    BaseStrategy            AutoScalerStrategy
    LastScaleUpTime         time.Time
    LastScaleDownTime       time.Time
    ScaleUpCooldown         time.Duration
    ScaleDownCooldown       time.Duration
    consecutiveScaleUpCount int
    consecutiveScaleDownCount int
    MinConsecutiveCount     int
}
```

**收益**:
- ✅ 避免频繁扩缩容导致的系统抖动
- ✅ 降低扩缩容开销
- ✅ 提升系统稳定性

---

#### 2. GradualScalingStrategy (渐进式扩缩容)
**优先级**: ⭐⭐⭐⭐☆

**实现方式**: 包装现有策略，控制每次扩缩容的步长

**功能描述**:
- 避免一次扩容/缩容过多
- 每次只调整少量Worker，观察效果
- 支持固定步长或百分比步长

**配置参数**:
```yaml
StepSize: 2                # 固定步长（每次增减2个）
MaxStepPercent: 0.25       # 百分比步长（每次最多调整当前数量的25%）
MinStep: 1                 # 最小步长
MaxStep: 5                 # 最大步长
```

**实现要点**:
```go
type GradualScalingWrapper struct {
    BaseStrategy   AutoScalerStrategy
    StepSize       int     // 固定步长
    MaxStepPercent float64 // 百分比步长
    MinStep        int
    MaxStep        int
}

func (g *GradualScalingWrapper) calculateStep(currentWorkers int, targetChange int) int {
    // 计算实际调整数量
    step := targetChange
    
    // 限制百分比
    maxByPercent := int(float64(currentWorkers) * g.MaxStepPercent)
    if step > maxByPercent {
        step = maxByPercent
    }
    
    // 限制范围
    if step < g.MinStep {
        step = g.MinStep
    }
    if step > g.MaxStep {
        step = g.MaxStep
    }
    
    return step
}
```

**收益**:
- ✅ 平滑过渡，避免剧烈波动
- ✅ 减少扩容过度导致的资源浪费
- ✅ 更安全可控

---

### 📊 中优先级（可选增强）

#### 3. MultiMetricStrategy (多指标联合策略)
**优先级**: ⭐⭐⭐☆☆

**功能描述**:
- 综合多个指标进行决策
- 支持加权评分模式
- 支持any/all逻辑

**可监控指标**:
- 队列平均长度
- 队列最大长度（P95/P99）
- 消息处理速率（QPS）
- Worker空闲率
- 消息平均等待时间

**配置参数**:
```yaml
Metrics:
  - Name: "avg_queue_length"
    Threshold: 50
    Weight: 0.4
  - Name: "p95_queue_length"
    Threshold: 100
    Weight: 0.3
  - Name: "idle_worker_ratio"
    Threshold: 0.3
    Weight: 0.3
AggregationMode: "weighted"  # "any" / "all" / "weighted"
```

**实现要点**:
```go
type MetricConfig struct {
    Name      string
    Threshold float64
    Weight    float64
    Collector func([]inf.IMailboxWorker) float64
}

type MultiMetricStrategy struct {
    Metrics         []MetricConfig
    AggregationMode string
}
```

**收益**:
- ✅ 更全面的负载评估
- ✅ 减少单一指标的误判
- ✅ 更精准的扩缩容决策

---

#### 4. SmoothingStrategy (时间窗口平滑策略)
**优先级**: ⭐⭐⭐☆☆

**功能描述**:
- 基于滑动窗口的平均值决策
- 过滤短时波动
- 识别持续趋势

**配置参数**:
```yaml
WindowSize: 10             # 窗口大小（检查次数）
HighThreshold: 80          # 高负载阈值
LowThreshold: 20           # 低负载阈值
ScaleUpConsecutive: 3      # 连续N次超过高阈值才扩容
ScaleDownConsecutive: 5    # 连续N次低于低阈值才缩容
```

**实现要点**:
```go
type SmoothingStrategy struct {
    WindowSize         int
    HighThreshold      int
    LowThreshold       int
    ScaleUpConsecutive int
    ScaleDownConsecutive int
    history            []int  // 历史负载记录
    historyIndex       int
}
```

**收益**:
- ✅ 避免因瞬时波动导致的误判
- ✅ 识别持续的负载趋势
- ✅ 更稳定的扩缩容行为

---

### 🎯 特殊场景（按需实现）

#### 5. EventDrivenStrategy (事件驱动策略)
**优先级**: ⭐⭐☆☆☆

**适用场景**:
- 外部系统触发扩缩容
- 定时任务前预扩容
- 依赖服务状态变化

**功能描述**:
- 订阅外部事件
- 根据事件类型触发扩缩容
- 支持手动触发

**配置参数**:
```yaml
Events:
  - Type: "scheduled_task_start"
    Action: "scale_up"
    TargetWorkers: 10
  - Type: "off_peak_time"
    Action: "scale_down"
    TargetWorkers: 2
```

**实现要点**:
```go
type ScalingEvent struct {
    Type          string
    Action        string  // "scale_up" / "scale_down"
    TargetWorkers int
}

type EventDrivenStrategy struct {
    EventChannel  chan ScalingEvent
    EventHandlers map[string]func(ScalingEvent) (int, bool)
}
```

---

#### 6. ThresholdRangeStrategy (阈值区间策略)
**优先级**: ⭐⭐☆☆☆

**功能描述**:
- 定义多个负载区间
- 不同区间对应不同Worker数量
- 类似阶梯函数

**配置示例**:
```yaml
Ranges:
  - LoadRange: [0, 50]
    TargetWorkers: 2
  - LoadRange: [51, 100]
    TargetWorkers: 4
  - LoadRange: [101, 200]
    TargetWorkers: 8
  - LoadRange: [201, 999999]
    TargetWorkers: 16
```

---

## ❌ 不推荐实现的策略

### TimeScheduledStrategy (时间计划策略)
**不推荐原因**:
- Actor邮箱是通用组件，不应耦合业务时间规律
- 时间规律应由上层业务系统控制
- 如需此功能，建议通过EventDrivenStrategy实现

### PredictiveStrategy (预测性扩缩容)
**不推荐原因**:
- 实现复杂度极高（需要机器学习）
- 需要历史数据存储和分析
- Actor邮箱负载变化随业务，难以预测
- 收益不明确

### CostOptimizedStrategy (成本优化策略)
**不推荐原因**:
- 进程内Worker扩缩容不涉及云资源费用
- 此策略更适合容器/虚拟机级别的扩缩容

---

## 实现建议

### 推荐实现顺序
1. **CooldownStrategy** - 立即解决抖动问题
2. **GradualScalingStrategy** - 平滑扩缩容过程
3. **MultiMetricStrategy** - 增强决策精准度
4. **SmoothingStrategy** - 进一步提升稳定性

### 设计原则
- ✅ 使用**装饰器模式**，不修改现有代码
- ✅ 所有策略应可**组合使用**
- ✅ 配置参数应有**合理默认值**
- ✅ 添加**详细日志**便于调试
- ✅ 提供**配置示例**和**最佳实践**

### 测试要点
- 模拟突发流量
- 验证冷却期是否生效
- 检查是否存在震荡
- 压测不同负载模式下的表现

---

## 参考资料

### 相关文件
- `strategy.go` - 当前策略实现
- `strategy_factory.go` - 策略工厂
- `scaler.go` - 扩缩容执行器
- `worker_pool.go` - Worker池管理

### 配置示例位置
- `engine/template/config/node.yaml` - 节点配置示例

---

**最后更新**: 2025-11-27

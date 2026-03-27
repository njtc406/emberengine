# Per-Node TraceID 生成器

> 最后更新：2026-03-27  
> 状态：待实施

## 背景

当前 `NewTraceID()` 使用包级全局 `atomic.Uint64` 作为序号源，格式为 `hex(nano)-hex(seq)`。

在"单进程单 Node"模型下此设计可用，但项目已完成 Node 自包含改造（全局变量清零），
现在单进程可运行多个 Node。全局 `traceSeq` 存在以下问题：

1. **跨进程碰撞**：两个进程各自 seq 从 0 开始，同一纳秒可生成相同 traceID
2. **无 Node 归属**：traceID 无法标识来源 Node，多 Node 日志排查困难
3. **全局残留**：与 Node 自包含架构方向矛盾

### nodeUid 说明

集群模式下，`nodeUid` 在 `EndpointManager` 初始化时通过 `uuid.NewString()` 自动生成，
用于简化 K8s 等编排环境的配置（无需为每个 Pod 分配不同 nodeId）。
每次 Node 启动 UUID 不同，仅标识唯一性，无其他语义。

因此 traceID 前缀 **不直接使用 nodeUid**（UUID 长 36 字符，会导致 traceID 过长），
而是对 nodeUid 做 **FNV-1a 32-bit 哈希**，取固定 8 字符 hex 作为前缀。

## 方案

### 核心设计：Per-Node TraceIDGenerator

每个 Node 持有独立的 `TraceIDGenerator`，以 `FNV32(nodeUid)` 的 8 位 hex 作为前缀。

```
格式：{fnv32hex(nodeUid)}-{hex(nano)}-{hex(seq)}
示例：a3f1b2c0-17a07e7b4a1bc00-3f
```

#### 前缀生成方式

对 `nodeUid`（UUID 字符串）做 FNV-1a 32-bit 哈希，输出固定 8 字符十六进制（前导零填充）。

| 属性 | 值 |
|------|---|
| 哈希算法 | FNV-1a 32-bit（`hash/fnv` 标准库） |
| 输出长度 | 固定 8 hex 字符 |
| 计算时机 | Node 启动时算一次，之后复用 |
| 运行时开销 | 零（前缀已预计算为 string） |

#### 唯一性保证

| 层级 | 保证方式 |
|------|---------|
| 同 Node | seq 单调递增 → **严格零冲突** |
| 同进程不同 Node | 各 Generator 独立 seq → 需要 FNV32 碰撞 **且** 同一纳秒 **且** 同一 seq 才碰撞 → **实质为零** |
| 跨进程 | 同上，不同进程启动时间不同，时间戳无法对齐 → **实质为零** |

#### FNV32 前缀碰撞概率（生日悖论）

$$P \approx \frac{N^2}{2 \times 2^{32}}$$

| Node 数 | 前缀碰撞概率 |
|---------|-------------|
| 10 | $1.2 \times 10^{-8}$ |
| 100 | $1.2 \times 10^{-6}$ |
| 1000 | $1.2 \times 10^{-4}$ |

即使前缀碰撞，还需同时满足"同一纳秒 + 同一 seq"，综合 traceID 碰撞概率为天文数字级别低。

#### 长度分析

| 段 | 长度 |
|---|------|
| FNV32 hex | **固定 8 字符** |
| hex(nano) | 15~16 字符 |
| hex(seq) | 1~8 字符 |
| 分隔符 | 2 字符 |
| **合计** | **~28 字符**（典型），最大 34 |

对比：UUID 36 字符，OTel TraceID 32 字符，当前实现 ~20 字符。

#### 性能

与当前实现持平：栈上固定缓冲 + `atomic.Add` + `time.Now()`，无 `crypto/rand` 系统调用。
FNV32 哈希仅在 Node 启动时计算一次（~5ns），不在 hot path 上。

## 变更

### 新增

| # | 文件 | 说明 |
|---|------|------|
| 1 | `engine/pkg/utils/emberctx/traceid.go` | `TraceIDGenerator` 结构体及 `NewTraceID()` 方法 |

### 修改

| # | 文件 | 说明 |
|---|------|------|
| 2 | `engine/pkg/interfaces/INodeContext.go` | `INodeContext` 增加 `GetTraceIDGenerator()` |
| 3 | `engine/pkg/node/node.go` | Node 初始化时创建 TraceIDGenerator |
| 4 | `engine/pkg/utils/emberctx/context.go` | `NewCtx` 增加接受 generator 的 Option；保留默认 fallback |
| 5 | `engine/pkg/utils/xcontext/context.go` | `ContextFactory` 持有 generator 引用 |
| 6 | 各调用点 | 有 INodeContext 的位置改用 generator；无 INodeContext 的保持 fallback |

### 删除

| # | 文件 | 说明 |
|---|------|------|
| 7 | `engine/pkg/utils/emberctx/context.go` | 移除全局 `traceSeq`（迁移完成后） |

## 核心代码

### FNV32 前缀生成

```go
// engine/pkg/utils/emberctx/traceid.go

package emberctx

import (
    "hash/fnv"
    "strconv"
    "sync/atomic"
    "time"
)

// newPrefix 对 nodeUid 做 FNV-1a 32-bit 哈希，返回固定 8 字符十六进制前缀。
// 仅在 Node 启动时调用一次。
func newPrefix(nodeUid string) string {
    h := fnv.New32a()
    h.Write([]byte(nodeUid))
    v := h.Sum32()
    var buf [8]byte
    const hex = "0123456789abcdef"
    for i := 7; i >= 0; i-- {
        buf[i] = hex[v&0xf]
        v >>= 4
    }
    return string(buf[:])
}
```

### TraceIDGenerator

```go
// TraceIDGenerator 为每个 Node 提供独立的 traceID 生成器。
// 格式: {fnv32hex}-{hex(nano)}-{hex(seq)}
// fnv32hex 是 nodeUid 的 FNV-1a 32-bit 哈希（固定 8 字符）。
type TraceIDGenerator struct {
    prefix string        // newPrefix(nodeUid) 的结果，固定 8 字符
    nodeUid string       // 原始 nodeUid，用于日志关联
    seq    atomic.Uint64
}

func NewTraceIDGenerator(nodeUid string) *TraceIDGenerator {
    return &TraceIDGenerator{
        prefix:  newPrefix(nodeUid),
        nodeUid: nodeUid,
    }
}

// GetPrefix 返回 8 字符 hex 前缀，用于日志中显示。
func (g *TraceIDGenerator) GetPrefix() string { return g.prefix }

// GetNodeUid 返回原始 nodeUid，用于从 prefix 反查完整 UUID。
func (g *TraceIDGenerator) GetNodeUid() string { return g.nodeUid }

func (g *TraceIDGenerator) NewTraceID() string {
    n := uint64(time.Now().UnixNano())
    s := g.seq.Add(1)
    // 固定: prefix(8) + '-' + nano(≤16) + '-' + seq(≤16) = ≤42
    var tmp [48]byte
    buf := tmp[:0]
    buf = append(buf, g.prefix...)
    buf = append(buf, '-')
    buf = strconv.AppendUint(buf, n, 16)
    buf = append(buf, '-')
    buf = strconv.AppendUint(buf, s, 16)
    return string(buf)
}
```

Node 启动时应打印关联日志，便于排查时从 prefix 反查 nodeUid：

```go
gen := NewTraceIDGenerator(nodeUid)
logger.Infof("traceID generator: prefix=%s nodeUid=%s", gen.GetPrefix(), gen.GetNodeUid())
// 输出: traceID generator: prefix=a3f1b2c0 nodeUid=550e8400-e29b-41d4-a716-446655440000
```

### INodeContext 扩展

```go
// engine/pkg/interfaces/INodeContext.go 新增方法

type INodeTraceIDGenerator interface {
    NewTraceID() string
}

type INodeContext interface {
    // ... 现有方法 ...

    // GetTraceIDGenerator 返回本 Node 的 traceID 生成器
    GetTraceIDGenerator() INodeTraceIDGenerator
}
```

### NewCtx 适配

```go
// WithTraceIDGenerator 允许注入 per-node 生成器
func WithGenerator(gen INodeTraceIDGenerator) Option {
    return func(ctx context.Context) context.Context {
        traceId := GetHeaderValue(ctx, def.DefaultTraceIdKey)
        if traceId == nil || traceId == "" {
            ctx = AddHeader(ctx, def.DefaultTraceIdKey, gen.NewTraceID())
        }
        return ctx
    }
}
```

### 兼容策略

```go
// 保留全局 fallback generator，用于无 INodeContext 的场景（系统启动早期、测试等）。
// fallback 使用 "unknown" 的 FNV32 哈希作为前缀，便于日志中识别出未归属的 trace。
var fallbackGenerator = NewTraceIDGenerator("unknown")

func NewCtx(ctx context.Context, options ...Option) context.Context {
    if ctx == nil {
        ctx = context.Background()
    }

    for _, option := range options {
        ctx = option(ctx)
    }

    // 如果 options 中没有设置 traceID，使用 fallback
    traceId := GetHeaderValue(ctx, def.DefaultTraceIdKey)
    if traceId == nil || traceId == "" {
        ctx = AddHeader(ctx, def.DefaultTraceIdKey, fallbackGenerator.NewTraceID())
    }
    return ctx
}
```

## 迁移策略

分两步迁移，保证零中断：

### 阶段一：并存

1. 新增 `TraceIDGenerator`、`INodeTraceIDGenerator`
2. `NewCtx` 使用 fallback（行为与当前完全一致）
3. 有 `INodeContext` 的 **高频调用点** 优先切换（`ContextFactory`、`bus.go` 等）
4. 全量跑通测试 + benchmark

### 阶段二：清理

1. 确认所有调用点已迁移或使用 fallback
2. 删除旧的全局 `traceSeq`
3. 日志中搜索 fallback generator 的固定前缀（`NewTraceIDGenerator("unknown")` 的 FNV32 值），核实是否存在遗漏

## 未来：OTel 集成规划

当可观测性需求明确后（Jaeger/Tempo/Grafana 接入），采用**分层共存**架构：

```
┌──────────────────────────────────────────────────┐
│                  外部边界入口                       │
│         (Gateway / 外部 gRPC Interceptor)          │
│                                                    │
│    生成 W3C TraceID (128-bit)                      │
│    写入 header["otel.traceId"]                     │
└────────────────────┬─────────────────────────────┘
                     │
┌────────────────────▼─────────────────────────────┐
│                  内部 Actor / RPC                   │
│                                                    │
│    使用 Per-Node TraceID (fnv32hex-nano-seq)      │
│    读取 header["ember.traceId"]                    │
│                                                    │
│    两者通过 header map 共存，不互相覆盖             │
└──────────────────────────────────────────────────┘
```

### 预留的扩展点

| 扩展点 | 说明 |
|--------|------|
| `def.OTelTraceIdKey` | 预留常量位（暂不定义），供 OTel traceID 使用 |
| `INodeTraceIDGenerator` | 窄接口设计，未来可替换为 OTel-compatible 实现 |
| Header map | 已支持 `map[string]any`，可同时携带多种 traceID |
| `ContextFactory` | 工厂模式，可扩展为同时写入两种 traceID |

### OTel 集成时的预估变更

| 变更 | 范围 |
|------|------|
| 新增 `otel` 依赖 | `go.mod` |
| 新增 gRPC Interceptor | 外部边界入口 |
| 新增 OTel Exporter | 新包 `engine/pkg/trace/` |
| Header 传播 | 在现有 `ToHeadersFast` 中追加 OTel 字段 |
| 内部 TraceID | **无需改动** |

关键原则：**内部 TraceID 是高频 hot path 产物，永远保持轻量；OTel TraceID 仅在边界生成和传播。**

# TraceID 跨进程唯一性改造

> 最后更新：2026-03-31  
> 状态：待实施

## 背景

当前 `NewTraceID()` 使用包级全局 `atomic.Uint64` 作为序号源，格式为 `hex(nano)-hex(seq)`。

单进程内 traceID 唯一，但跨进程存在碰撞风险：两个进程各自 seq 从 0 开始，
同一纳秒可生成相同 traceID。

## 方案：进程级随机前缀

进程启动时用 `crypto/rand` 生成 4 字节随机前缀，所有 Node 共享。
**零 API 变更，零注入，改 1 个文件**。

```
格式：{prefix}-{hex(nano)}-{hex(seq)}
示例：a3f1b2c0-17a07e7b4a1bc00-3f
```

### 唯一性

| 层级 | 保证方式 |
|------|---------|
| 同进程 | 共享 atomic seq 单调递增 → **严格零冲突** |
| 跨进程 | 32-bit 随机前缀 + 不同 nano + 不同 seq → **实质为零** |

### 长度

| 段 | 长度 |
|---|------|
| prefix | 固定 8 字符 |
| hex(nano) | 15~16 字符 |
| hex(seq) | 1~8 字符 |
| 分隔符 | 2 字符 |
| **合计** | **~28 字符**（典型） |

### 性能

`crypto/rand` 仅在 `init` 时调用一次。hot path 与当前完全一致：栈上缓冲 + `atomic.Add` + `time.Now()`。

## 变更

| # | 文件 | 说明 |
|---|------|------|
| 1 | `engine/pkg/utils/emberctx/context.go` | 替换全局 `traceSeq` 为带随机前缀的 generator |
| 2 | `engine/pkg/utils/xcontext/context.go` | **修复 `NewWithCloneCtx` 返回值丢弃 bug** |

**不变**：`NewTraceID()` 函数签名不变，所有调用点零修改。
`ContextFactory.NewContext()` 调用 `emberctx.NewTraceID()` → 签名未变，**无需修改**。
`NewCtx()` 内部调用 `NewTraceID()` → 签名未变，**无需修改**。

## 核心代码

```go
// engine/pkg/utils/emberctx/context.go

import (
    crypto_rand "crypto/rand"
    "strconv"
    "sync/atomic"
    "time"
)

var (
    tracePrefix string
    traceSeq    atomic.Uint64
)

func init() {
    var buf [4]byte
    if _, err := crypto_rand.Read(buf[:]); err != nil {
        panic("emberctx: failed to read crypto/rand: " + err.Error())
    }
    // 手动编码为 8 字符 hex，避免引入 encoding/hex
    const hexDigits = "0123456789abcdef"
    var prefix [8]byte
    for i, b := range buf {
        prefix[i*2] = hexDigits[b>>4]
        prefix[i*2+1] = hexDigits[b&0x0f]
    }
    tracePrefix = string(prefix[:])
}

func NewTraceID() string {
    n := uint64(time.Now().UnixNano())
    s := traceSeq.Add(1)
    var tmp [48]byte
    b := tmp[:0]
    b = append(b, tracePrefix...)
    b = append(b, '-')
    b = strconv.AppendUint(b, n, 16)
    b = append(b, '-')
    b = strconv.AppendUint(b, s, 16)
    return string(b)
}
```

## NewWithCloneCtx Bug 修复

**⚠️ 已有 Bug**：`NewWithCloneCtx()` 中 `AddHeaders` 返回值被丢弃，
克隆出的 context 丢失所有旧 headers（dispatchKey、priority 等）。

```go
// 当前代码（有 bug）:
newCtx := emberctx.NewCtx(context.Background())
emberctx.AddHeaders(newCtx, headers)        // ← 返回值丢弃
return XContext{Context: newCtx}            // 旧 headers 全丢

// 修复:
newCtx := emberctx.NewCtx(context.Background())
newCtx = emberctx.AddHeaders(newCtx, headers) // ← 接收返回值
return XContext{Context: newCtx}
```

此 bug 应在本次改造中一并修复。

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
│    使用内部 TraceID (prefix-nano-seq)              │
│    读取 header["ember.traceId"]                    │
│                                                    │
│    两者通过 header map 共存，不互相覆盖             │
└──────────────────────────────────────────────────┘
```

### 预留的扩展点

| 扩展点 | 说明 |
|--------|------|
| `def.OTelTraceIdKey` | 预留常量位（暂不定义），供 OTel traceID 使用 |
| Header map | 已支持 `map[string]any`，可同时携带多种 traceID |
| `ContextFactory` | 工厂模式，可扩展为同时写入 OTel traceID |

关键原则：**内部 TraceID 是高频 hot path 产物，永远保持轻量；OTel TraceID 仅在边界生成和传播。**

# node_concurrency — 并发压测场景

## 场景说明

`node_concurrency` 是 EmberEngine 的高并发 RPC 压测工具。它注册一个 `ConcurrencyTest` 服务，在启动后使用可配置数量的并发 worker 向远端 `ConcurrencyTest1` 服务发送大量 RPC 请求，测量 QPS、延迟分布和错误率。

支持的 RPC 模式：`send`（单向）、`call`（同步）、`asyncCall`（异步回调），以及模拟真实业务场景的 `userData` 和 `battle`。

---

## 前置条件

| 依赖 | 说明 |
|------|------|
| etcd | 用于服务发现，默认 `${REMOTE_HOST}:2379` |
| NATS | 若使用 nats RPC 类型 |
| node_concurrency1 | 接收端节点，需先启动 |

---

## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `REMOTE_HOST` | `192.168.145.188` | etcd / NATS 服务器地址 |
| `BENCH_MODE` | `workers` | 压测模式：`workers`（固定并发 worker）/ `goroutine`（传统模式） |
| `BENCH_TOTAL` | `100000` | 总请求数 |
| `BENCH_CONCURRENCY` | `500` | 并发 worker 数 |
| `BENCH_TYPE` | `send` | RPC 类型：`send` / `call` / `asyncCall` / `userData` / `battle` |
| `BENCH_RECORD_DURATIONS` | `0` | 是否记录每次请求耗时（`1` = 开启，输出 P50/P90/P99） |
| `BENCH_TRACK_SUCCESS_TS` | `0` | 是否追踪最后成功时间（`1` = 开启） |
| `BENCH_PER_REQ_TRACE` | `0` | 是否每请求生成 TraceID（`1` = 开启，降低吞吐） |
| `GOGC` | `400` | GC 触发阈值（减少 GC 频率） |
| `POOL_STATS` | `0` | 是否启用对象池统计 |
| `META_LEAK_TRACK` | `0` | 是否启用元数据泄漏追踪 |

---

## 启动命令

### 1. 启动接收端

```powershell
$env:REMOTE_HOST = '192.168.145.188'
go run ./example/node_concurrency1
```

### 2. 启动发送端（压测）

```powershell
# 基础 Send 压测（10万次，500并发）
$env:REMOTE_HOST = '192.168.145.188'
$env:BENCH_MODE = 'workers'
$env:BENCH_TOTAL = '100000'
$env:BENCH_CONCURRENCY = '500'
$env:BENCH_TYPE = 'send'
go run ./example/node_concurrency
```

```powershell
# 同步 Call 压测（带延迟统计）
$env:BENCH_TYPE = 'call'
$env:BENCH_RECORD_DURATIONS = '1'
go run ./example/node_concurrency
```

```powershell
# 大规模 pprof 采集（2000万次，不记录耗时）
$env:BENCH_TOTAL = '20000000'
$env:BENCH_TYPE = 'send'
$env:BENCH_RECORD_DURATIONS = '0'
go run ./example/node_concurrency
```

---

## 预期输出与指标

压测完成后输出：

```
========== Bench Result ==========
bench mode:    workers
test type:     send
total:         100000
concurrency:   500
elapsed:       1.234s
QPS:           81037
errors:        0
```

当 `BENCH_RECORD_DURATIONS=1` 时，额外输出：

```
P50:           12µs
P90:           45µs
P95:           78µs
P99:           234µs
Max:           1.2ms
```

---

## 配合 pprof 使用

发送端默认在 `:6060` 端口开启 pprof，接收端在 `:6061`。

```powershell
# 采集 CPU Profile（20秒）
Invoke-WebRequest http://127.0.0.1:6060/debug/pprof/profile?seconds=20 -OutFile cpu.pb.gz
go tool pprof -http=:8080 cpu.pb.gz

# 采集 Heap Profile
Invoke-WebRequest http://127.0.0.1:6060/debug/pprof/heap -OutFile heap.pb.gz
go tool pprof -http=:8080 heap.pb.gz
```

---

## 相关文件

| 文件 | 说明 |
|------|------|
| `example/node_concurrency/main.go` | 发送端入口 |
| `example/node_concurrency1/main.go` | 接收端入口 |
| `example/comm/concurrency.go` | 压测核心逻辑 |
| `example/configs/node_concurrency/` | 发送端配置 |
| `example/configs/node_concurrency1/` | 接收端配置 |
| `tools/pprof/capture_cpu.ps1` | pprof 采集脚本 |

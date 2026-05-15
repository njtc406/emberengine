# P0 稳定性开发文档

> 创建时间：2026年5月12日  
> 来源：`docs/ROADMAP.md` 的 P0 任务与当前推进计划 Phase 1  
> 目标：把 P0 级稳定性问题拆成可开发、可测试、可验收的任务单

---

## 一、P0 总目标

P0 阶段只解决框架稳定性问题，不扩展新能力。核心目标是证明 Actor/Mailbox/RPC/Config 的基础链路在成功、失败、超时、拒绝、停止和并发场景下都具备确定行为。

验收标准：

1. Envelope/Job/Pool/Bus/Context 的所有权边界明确，无重复释放、漏释放和悬挂引用。
2. Mailbox 的 Submit/Drain/Stop/Suspend/Resume/RW 分离流程可重复验证，无死锁、goroutine 泄漏和 CPU 空转。
3. RPC 的 Call/AsyncCall/Send 在本地、远程、异常路径下释放资源一致。
4. Actor 重整后的关键契约被测试保护，避免后续回归。
5. 模板配置和全部 example 配置持续通过 `Config.Load` 回归测试。

---

## 二、P0 任务状态

| 编号 | 问题 | 当前状态 | 本文档处理方式 |
|------|------|----------|----------------|
| P0-1 | 资源释放规范统一 | ✅ 已完成 | 所有权审计完成、释放路径测试覆盖、修复 3 个真实 bug |
| P0-2 | Mailbox 流程稳定 | ✅ 已完成 | 生命周期闭环测试覆盖 Stop/Drain/Suspend/panic/multi-worker |
| P0-3 | RPC 调用链完善 | ✅ 已完成 | CallState/Monitor 回归测试覆盖、修复 monitor race |
| P0-4 | Actor 目录重整 | ✅ 已完成 | 保留为回归门禁，保护 RW/中间件/配置契约 |
| P0-5 | 配置基线回归 | ✅ 已完成 | 负向测试覆盖缺字段/非法值/空文件、修复 nil panic |

---

## 三、开发原则

1. **先测试暴露问题，再做最小修复**：P0 不做大规模重构。
2. **所有权优先**：每个资源必须能回答“谁创建、谁转移、谁最后释放”。
3. **停止流程优先证明无泄漏**：Stop/Drain/Wait 必须覆盖正常和异常路径。
4. **配置和示例是回归资产**：新增配置字段必须同步模板、示例和测试。
5. **不引入新的全局运行时状态**：遵循 Node 自包含和 INodeContext 注入模式。

---

## 四、P0-1：资源释放规范统一

### 问题描述

当前框架存在多类可复用或需释放对象：Envelope、Job、Pool 对象、Bus、Context、CallState。P0-1 要把所有成功和失败路径的最后持有者固定下来，并用测试证明不会漏释放或重复释放。

### 重点文件

| 文件 | 关注点 |
|------|--------|
| `engine/pkg/interfaces/IEnvelope.go` | Envelope/Meta/Data 生命周期契约 |
| `engine/pkg/interfaces/IMailBox.go` | PostJob、OnJobDiscarded、BeginStop/Wait 契约 |
| `engine/pkg/interfaces/IRpcClient.go` | DeliverRequest/DeliverResponse 所有权转移 |
| `engine/pkg/actor/mailbox/job/` | Job.Release 与 payload 释放 |
| `engine/pkg/actor/mailbox/worker_pool.go` | submit、discard、drain、stop 路径 |
| `engine/pkg/rpc/message/` | MessageBus Call/AsyncCall/Send |
| `engine/pkg/rpc/remote/` | 远程请求/响应处理 |
| `engine/pkg/monitor/call_state.go` | 超时、迟到响应、回调释放 |

### 所有权规则

```text
Envelope 所有权规则：
├── 本地请求：创建者 → Deliver → Job.Release() 统一释放 payload
├── 本地回复：handler 创建 respEnv → sender_local 释放
├── 远程请求：创建者 → remote sender 释放
└── 远程回复：handler 创建 → remote sender 释放

核心规则：
1. Job 释放时必须释放 payload。
2. 谁最后持有 envelope，谁负责释放。
3. rejected/discarded/timeout 路径必须和 success 路径同样可验证。
```

### 开发任务

| 子任务 | 内容 | 产物 |
|--------|------|------|
| P0-1.1 | 画出 Envelope/Job/Bus/CallState 全路径所有权表 | 测试注释或文档表格 |
| P0-1.2 | 补齐 Job.Release 释放 payload 的单元测试 | `actor/mailbox/job` 测试 |
| P0-1.3 | 覆盖 PostJob 成功、拒绝、discard 三条路径 | `actor/mailbox` 测试 |
| P0-1.4 | 覆盖 RPC 本地请求/回复释放路径 | `rpc/message` 或 `core/rpc` 测试 |
| P0-1.5 | 覆盖远程请求失败、连接失败、超时路径 | `rpc/remote` 或 `rpc/client` 测试 |
| P0-1.6 | 检查 CallState 超时后迟到响应处理 | `monitor` 测试 |

### 验收标准

- `go test ./engine/pkg/actor/... -count=1` 通过。
- `go test ./engine/pkg/rpc/... ./engine/pkg/monitor/... -count=1` 通过。
- `go test -race ./engine/pkg/actor/... ./engine/pkg/rpc/...` 通过或明确记录现有阻塞点。
- 任何新增释放逻辑都有“成功 + 失败/拒绝/超时”测试。

---

## 五、P0-2：Mailbox 流程稳定

### 问题描述

Mailbox 是 Actor 调度核心。P0-2 要保证 submit、drain、stop、RW 分离、中间件链和 worker 生命周期稳定。重点不是提升性能，而是证明并发行为可预测。

### 重点文件

| 文件 | 关注点 |
|------|--------|
| `engine/pkg/actor/mailbox/mailbox.go` | Mailbox 门面与生命周期 |
| `engine/pkg/actor/mailbox/worker_pool.go` | WorkerPool 启停、提交、drain |
| `engine/pkg/actor/mailbox/worker.go` | Worker 退出与任务执行 |
| `engine/pkg/actor/mailbox/queue_manager*.go` | 队列 submit/pop/drain 行为 |
| `engine/pkg/actor/mailbox/rw_controller.go` | RW 分离与 in-flight read 追踪 |
| `engine/pkg/actor/mailbox/stop_policy.go` | GraceTimeout + DrainPolicy |
| `engine/pkg/actor/mailbox/middleware_chain.go` | 洋葱中间件顺序与错误传播 |
| `engine/pkg/actor/mailbox/rate_limit_middleware.go` | 限流拒绝路径 |
| `engine/pkg/actor/mailbox/sentinel_middleware.go` | Sentinel 熔断/限流路径 |

### 开发任务

| 子任务 | 内容 | 产物 |
|--------|------|------|
| P0-2.1 | BeginStop/Wait/Stop 幂等测试 | mailbox 生命周期测试 |
| P0-2.2 | DrainPolicy=execute：停止时执行剩余任务 | worker_pool 测试 |
| P0-2.3 | DrainPolicy=discard：停止时丢弃剩余任务并触发回调 | worker_pool 测试 |
| P0-2.4 | Suspend/Resume：挂起后拒绝非紧急任务，恢复后正常提交 | mailbox 测试 |
| P0-2.5 | RW mode：读任务并发，写任务互斥，写等待读完成 | `worker_pool_rw_test.go` 扩展 |
| P0-2.6 | read dispatch channel 满时行为明确 | RW/dispatch 测试 |
| P0-2.7 | handler panic 后资源释放、worker 继续或退出策略明确 | worker 测试 |
| P0-2.8 | 中间件链顺序、错误短路、统计回调测试 | middleware 测试 |

### 验收标准

- `go test ./engine/pkg/actor/mailbox/... -count=1` 通过。
- `go test -race -count=2 ./engine/pkg/actor/...` 通过。
- Stop/Drain 测试不依赖 `time.Sleep` 的不稳定等待，优先使用 channel/WaitGroup/上下文超时。
- panic/discard/limit rejected 路径不泄漏 Job payload。

---

## 六、P0-3：RPC 调用链回归门禁

### 当前状态

P0-3 已完成，但仍需要作为 P0 门禁保留。后续改动 Mailbox、资源释放或路由时，必须保证 RPC 三种调用语义不回归。

### 回归范围

| 场景 | 需要验证 |
|------|----------|
| Call | 正常返回、超时、目标服务不存在、方法不存在、远端错误 |
| AsyncCall | 正常回调、错误回调、调用上下文取消、迟到响应 |
| Send | 单向发送成功、目标不可达、提交失败 |
| Local Sender | 本地请求与本地回复释放路径 |
| Remote Sender | 编码失败、连接失败、远端拒绝、响应解码失败 |
| Router | 指定节点、随机、广播、一致性哈希路由失败 |

### 开发任务

| 子任务 | 内容 |
|--------|------|
| P0-3.1 | 为 Call/AsyncCall/Send 建立最小 fake service 测试夹具 |
| P0-3.2 | 覆盖本地调用路径释放和错误传播 |
| P0-3.3 | 覆盖远程 sender 连接失败/超时路径 |
| P0-3.4 | 覆盖 late response 不触发二次回调或泄漏 |
| P0-3.5 | 将 RPC 回归纳入 P0 验证命令 |

### 验收标准

- `go test ./engine/pkg/rpc/... ./engine/pkg/core/... -count=1` 通过。
- RPC 失败路径返回错误包含上下文，可用于日志定位。
- Bus 使用后有明确释放策略，测试中不依赖全局状态。

---

## 七、P0-4：Actor 重整回归门禁

### 当前状态

Actor 目录重整已完成，包含 Mailbox RW 分离、洋葱中间件、panicRateLimiter、死代码清理、CPU spin 修复、RegisterJobFactory race 修复等。本项不再继续大重构，只补测试保护。

### 回归范围

| 能力 | 需要保护的行为 |
|------|----------------|
| RW 分离 | 读并发、写互斥、写等待读完成、in-flight read 归零 |
| 中间件链 | 顺序执行、错误短路、统计计数、dispatch key 统计 |
| 限流/熔断 | rejected 路径触发 discard/release，配置开关生效 |
| WorkerPool | 扩缩容、空闲策略、stop 后拒绝新任务 |
| JobFactory | 并发注册/读取无 race |

### 开发任务

| 子任务 | 内容 |
|--------|------|
| P0-4.1 | 汇总 actor 重整后的关键契约到测试注释 |
| P0-4.2 | 扩展 RW 测试，覆盖 read/write 交错和停止时 in-flight read |
| P0-4.3 | 扩展中间件链测试，覆盖错误传播和资源释放 |
| P0-4.4 | 扩展限流/熔断 rejected 路径测试 |
| P0-4.5 | 保持 `go test -race -count=2 ./engine/pkg/actor/...` 为必过门禁 |

---

## 八、P0-5：配置基线回归门禁

### 当前状态

配置基线回归已完成：模板和全部 example 配置可通过 `Config.Load`。本项后续重点是防止新增配置字段导致模板、示例和默认值再次分叉。

### 重点文件

| 文件/目录 | 关注点 |
|-----------|--------|
| `engine/pkg/config/define.go` | 配置结构体和 binding tags |
| `engine/pkg/config/config.go` | Load/Validate 流程 |
| `engine/pkg/config/config_test.go` | 配置回归测试 |
| `template/config/node.yaml` | 标准模板 |
| `example/configs/**/node.yaml` | 示例配置 |

### 开发任务

| 子任务 | 内容 |
|--------|------|
| P0-5.1 | 为 StopPolicy、MailboxConf、EventBusConf 增加非法值测试 |
| P0-5.2 | 为缺少必填字段增加失败测试 |
| P0-5.3 | 为模板和 example 配置维持正向回归测试 |
| P0-5.4 | 新增配置字段时同步模板注释和所有 example |
| P0-5.5 | 保持 `go test ./engine/pkg/config/... -count=1` 为必过门禁 |

### 验收标准

- `go test ./engine/pkg/config/... -count=1` 通过。
- 新增配置字段具备 binding tags 或明确默认值。
- 模板配置注释能解释字段用途、默认值和取值范围。

---

## 九、推荐实施顺序

```text
Step 1：P0-1 所有权审计
  └── 先固定 Envelope/Job/Bus/CallState 的最后持有者

Step 2：P0-2 Mailbox 生命周期测试
  └── BeginStop/Wait/DrainPolicy/Suspend/RW/panic 路径

Step 3：P0-3 RPC 回归测试
  └── Call/AsyncCall/Send 的成功、超时、错误、迟到响应

Step 4：P0-5 配置非法值测试
  └── 在已有配置基线测试基础上补负向测试

Step 5：全量验证与文档回填
  └── 更新 ROADMAP/NEXT_GOALS 状态，记录阻塞点
```

---

## 十、验证命令

### 最小验证

```powershell
go test ./engine/pkg/config/... -count=1
go test ./engine/pkg/actor/... -count=1
go test ./engine/pkg/rpc/... ./engine/pkg/monitor/... -count=1
```

### P0 完整验证

```powershell
go build ./...
go vet ./...
go test ./...
go test -race -count=2 ./engine/pkg/actor/...
go test -race ./engine/pkg/core/... ./engine/pkg/rpc/... ./engine/pkg/cluster/... ./engine/pkg/event/...
```

### 示例/压测验证

```powershell
# 短压：验证基础吞吐和无明显泄漏
$env:POOL_STATS='0'; $env:META_LEAK_TRACK='0'; $env:BENCH_MODE='workers'; $env:BENCH_TOTAL='100000'; $env:BENCH_CONCURRENCY='500'; $env:BENCH_TYPE='send'; go run ./example/node_concurrency

# 业务场景：userData / battle / call
$env:BENCH_RECORD_DURATIONS='1'; $env:BENCH_TYPE='userData'; go run ./example/node_concurrency
$env:BENCH_RECORD_DURATIONS='1'; $env:BENCH_TYPE='battle'; go run ./example/node_concurrency
$env:BENCH_RECORD_DURATIONS='1'; $env:BENCH_TYPE='call'; go run ./example/node_concurrency
```

---

## 十一、完成定义

P0 可以关闭的条件：

- [x] P0-1 所有权路径表完成，并有测试覆盖成功/失败/拒绝/超时路径。
- [x] P0-2 Mailbox 生命周期和 RW 分离测试覆盖关键路径，race 通过。
- [x] P0-3 RPC 三种调用语义作为回归门禁保留，失败路径行为明确。
- [x] P0-4 Actor 重整关键契约有测试保护。
- [x] P0-5 配置正向/负向回归测试通过。
- [x] `go build ./...`、`go vet ./...`、`go test ./...` 全部通过。
- [x] 文档回填 ROADMAP/NEXT_GOALS，将 P0-1/P0-2 状态更新为已完成或记录剩余阻塞。

---

## 十二、风险与处理

| 风险 | 影响 | 处理方式 |
|------|------|----------|
| race 测试暴露历史问题较多 | P0 周期拉长 | 先分类记录，优先修复资源释放和停止路径相关问题 |
| RPC 远程路径测试夹具复杂 | 测试成本高 | 先 fake sender/client，再补真实 node_concurrency 集成验证 |
| 配置字段继续增加 | 模板/example 容易漂移 | 新增字段必须同步配置回归测试 |
| Stop/Drain 测试偶发超时 | CI 不稳定 | 避免裸 `time.Sleep`，使用 channel/WaitGroup/上下文超时 |
| 资源释放测试难以观测 | 漏洞不易复现 | 使用 fake payload 计数器、atomic 计数、leak tracker 辅助验证 |

---

## 十三、具体开发流程

本节把 P0 从“任务清单”转成“开发执行顺序”。每一轮开发都按“小步验证、最小修复、状态回填”的方式推进，避免一次性改动过大导致问题混在一起。

### 13.1 总体节奏

| 阶段 | 目标 | 预计耗时 | 输出物 | 验证命令 |
|------|------|----------|--------|----------|
| D0：基线确认 | 确认当前主干可构建、可测试 | 0.5 天 | 基线测试结果、已知失败列表 | `go build ./...`; `go vet ./...`; `go test ./...` |
| D1：资源所有权审计 | 固定 Envelope/Job/Bus/CallState 的最后持有者 | 1 天 | 所有权路径表、P0-1 测试清单 | `go test ./engine/pkg/actor/... ./engine/pkg/rpc/... ./engine/pkg/monitor/... -count=1` |
| D2：测试夹具建设 | 建立可复用 fake payload/fake job/fake sender/helper | 1 天 | 测试 helper、释放计数器、超时断言工具 | 聚焦相关 package 测试 |
| D3：Mailbox 生命周期闭环 | 补齐 BeginStop/Wait/Drain/Suspend/RW/panic 测试与修复 | 2-3 天 | Mailbox 回归测试、必要修复 | `go test -race -count=2 ./engine/pkg/actor/...` |
| D4：RPC 调用链回归 | 补齐 Call/AsyncCall/Send 成功和异常路径 | 2-3 天 | RPC 回归测试、必要修复 | `go test ./engine/pkg/rpc/... ./engine/pkg/core/... -count=1` |
| D5：配置负向测试 | 在已有配置基线基础上补非法值/缺字段测试 | 1 天 | 配置负向测试、模板同步检查 | `go test ./engine/pkg/config/... -count=1` |
| D6：全量验证与回填 | 运行完整验证，更新 ROADMAP/NEXT_GOALS 状态 | 0.5-1 天 | 验证记录、文档状态更新 | P0 完整验证命令 |

### 13.2 每个子任务的开发循环

每个 P0 子任务都按以下闭环执行：

```text
1. 定位范围
  ├── 阅读任务对应的重点文件
  ├── 找出现有测试和相邻实现
  └── 明确成功路径、失败路径、停止路径

2. 先写测试
  ├── 正常路径：证明当前预期行为
  ├── 异常路径：拒绝、超时、panic、连接失败、上下文取消
  └── 资源路径：Release/Callback/Drain/Wait 必须可观测

3. 运行聚焦测试
  ├── 如果测试直接通过：记录为已覆盖
  └── 如果失败：确认是测试问题还是实现问题

4. 最小修复
  ├── 只修改当前问题相关文件
  ├── 不顺手重构无关逻辑
  └── 不引入新的全局状态或 panic

5. 回归验证
  ├── 先跑 package 级测试
  ├── 再跑相关 race 测试
  └── 最后按需跑全量 go test ./...

6. 状态回填
  ├── 更新本文档的任务状态或阻塞说明
  ├── 必要时更新 ROADMAP/NEXT_GOALS
  └── 记录新增验证命令或测试覆盖点
```

### 13.3 建议 PR / 提交拆分

| 批次 | 范围 | 目标 | 不包含 |
|------|------|------|--------|
| PR-0 | 基线验证 | 跑通 build/vet/test，记录当前已知问题 | 代码修复 |
| PR-1 | P0-1.1~P0-1.3 | 资源所有权表 + actor/job/mailbox 释放测试 | RPC 远程路径 |
| PR-2 | P0-2.1~P0-2.4 | Mailbox Stop/Drain/Suspend/Resume 生命周期测试与修复 | RW 大范围调整 |
| PR-3 | P0-2.5~P0-2.8 + P0-4 | RW、中间件、限流/熔断 rejected 路径回归 | RPC 语义调整 |
| PR-4 | P0-3 | Call/AsyncCall/Send 本地和远程异常路径回归 | Cluster/Router 大重构 |
| PR-5 | P0-5 | 配置负向测试、模板/example 同步保护 | 新增配置能力 |
| PR-6 | P0 收口 | 完整验证、文档回填、状态更新 | 新功能 |

如果不使用 PR，也应按上表作为本地提交粒度。每个批次都必须能独立通过对应聚焦测试。

### 13.4 文件级执行顺序

| 顺序 | 文件/目录 | 动作 |
|------|-----------|------|
| 1 | `engine/pkg/actor/mailbox/job/` | 先补 fake payload 和 Job.Release 释放测试 |
| 2 | `engine/pkg/actor/mailbox/queue_manager*.go` | 验证 submit、pop、drain、discard 的边界行为 |
| 3 | `engine/pkg/actor/mailbox/worker_pool.go` | 补 BeginStop/Wait/DrainPolicy/Suspend/Resume 测试 |
| 4 | `engine/pkg/actor/mailbox/rw_controller.go` | 补 RW in-flight read、写等待和停止路径测试 |
| 5 | `engine/pkg/actor/mailbox/middleware_chain.go` | 补中间件顺序、短路、错误传播测试 |
| 6 | `engine/pkg/rpc/message/` | 补 MessageBus Call/AsyncCall/Send 语义测试 |
| 7 | `engine/pkg/rpc/remote/` 与 `engine/pkg/rpc/client/` | 补连接失败、编码/解码失败、远端拒绝测试 |
| 8 | `engine/pkg/monitor/call_state.go` | 补超时和 late response 行为测试 |
| 9 | `engine/pkg/config/` | 补配置非法值、缺字段、模板/example 正向回归 |

### 13.5 测试夹具约定

P0 测试优先使用可观测的 fake 对象，而不是依赖日志或睡眠时间判断。

推荐 helper：

| Helper | 用途 |
|--------|------|
| `fakeReleasePayload` | 通过 atomic 计数验证 payload 是否释放一次 |
| `fakeMailboxJob` | 模拟成功、失败、panic、阻塞、只读/写任务 |
| `waitUntil(t, cond)` | 用 deadline 轮询替代裸 `time.Sleep` |
| `mustEventually(t, ch)` | 验证异步事件在超时前发生 |
| `fakeSender` / `fakeRpcClient` | 模拟本地/远程发送成功、失败、超时 |
| `fakeCallStateCallback` | 验证回调只触发一次，late response 不重复触发 |

测试规则：

1. 不使用无上限等待，所有异步测试都必须有 timeout。
2. `time.Sleep` 只能作为极短让步，不作为唯一断言手段。
3. 资源释放用计数器或 channel 断言，不靠人工观察日志。
4. 并发测试必须能在 `-race` 下稳定运行。
5. fake 对象只放在对应 package 的 `_test.go` 中，避免污染生产代码。

### 13.6 状态流转规则

| 状态 | 含义 | 进入条件 | 退出条件 |
|------|------|----------|----------|
| ⏳ 待开始 | 尚未编码 | 任务被列入计划 | 开始阅读代码或写测试 |
| 🔄 进行中 | 正在实现或验证 | 已有测试/修复分支 | 聚焦测试通过或发现阻塞 |
| ⚠️ 阻塞 | 无法继续推进 | 缺少测试夹具、race 暴露无关历史问题、远程依赖不可用 | 记录阻塞原因和替代路径 |
| ✅ 已完成 | 可关闭 | 聚焦测试 + 必要 race 测试通过 | 文档状态回填完成 |

每完成一个子任务，至少回填三项信息：

1. 变更文件。
2. 新增/修改的测试用例。
3. 实际运行过的验证命令及结果。

### 13.7 日常执行模板

每天开始前：

```text
1. git status：确认工作区变更范围
2. 选择一个 P0 子任务，不跨多个主题并行修改
3. 阅读相关文件和现有测试
4. 写下本轮要补的测试路径
```

每次提交前：

```text
1. go test <相关 package> -count=1
2. 涉及 actor/rpc 并发时运行 go test -race <相关 package>
3. 检查是否同步了配置模板/example（如涉及配置）
4. 检查是否新增了临时文件、日志文件、pprof 文件
5. 回填本文档或 ROADMAP/NEXT_GOALS 的状态
```

P0 收口前：

```text
1. go build ./...
2. go vet ./...
3. go test ./...
4. go test -race -count=2 ./engine/pkg/actor/...
5. go test -race ./engine/pkg/core/... ./engine/pkg/rpc/... ./engine/pkg/cluster/... ./engine/pkg/event/...
6. 更新 ROADMAP.md：P0-1/P0-2/P0-3/P0-4/P0-5 状态
7. 更新 NEXT_GOALS.md：Phase A/P0 进度和剩余风险
```

### 13.8 阻塞处理策略

遇到阻塞时不要扩大改动范围，按下列顺序处理：

1. **测试夹具不足**：先补 fake/helper，不直接改生产代码绕过测试。
2. **race 暴露无关问题**：记录为独立问题，只修当前资源/停止相关路径。
3. **远程 RPC 场景难搭建**：先 fake sender/client，再使用 `example/node_concurrency` 做集成验证。
4. **配置变更牵连过大**：先补负向测试证明问题，再分批同步模板和 example。
5. **修复需要架构调整**：暂停实现，交给架构师确认设计，不在 P0 内私自重构。

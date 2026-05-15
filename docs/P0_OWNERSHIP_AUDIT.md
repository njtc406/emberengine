# P0-1 资源所有权审计表

> 创建时间：2026年5月12日  
> 来源：P0_STABILITY_DEV_PLAN.md P0-1.1

---

## 一、Envelope 所有权路径

| 场景 | 创建者 | 所有权转移 | 最终释放者 | 文件 |
|------|--------|-----------|-----------|------|
| 本地 Call 请求 | `MessageBus.call()` | → `DeliverRequest()` → `localSender` 创建 `RpcJob` → `PostJob()` | 成功：Worker `safeExec()` → `Job.Release()` (Envelope 由 handler 管理) | `msgbus/bus.go` → `client/sender_local.go` |
| 本地 Call 请求失败 | `MessageBus.call()` | → `DeliverRequest()` 返回 err | `MessageBus.call()` 中 `envelope.Release()` | `msgbus/bus.go:321-322` |
| 本地 AsyncCall 请求 | `MessageBus.asyncCall()` | 同 Call 请求 | 同 Call 请求 | `msgbus/bus.go` |
| 本地 Send 请求 | `MessageBus.send()` | 同 Call 请求 | 同 Call 请求 | `msgbus/bus.go` |
| 本地 Send 请求失败 | `MessageBus.send()` | → `DeliverRequest()` 返回 err | `MessageBus.send()` 中 `envelope.Release()` | `msgbus/bus.go:571` |
| 本地 Call 回复 | `core/handler_job.go` handler | → `DeliverResponse()` → `localSender` | 同步 Call：`localSender.DeliverResponse()` 中 `envelope.Release()` | `client/sender_local.go:99` |
| 本地 AsyncCall 回复 | `core/handler_job.go` handler | → `localSender.DeliverResponse()` → 创建 RpcJob → `PostJob()` | Worker 执行 callback job → `Job.Release()` | `client/sender_local.go:78-88` |
| 本地回复 late response | `core/handler_job.go` handler | → `localSender.DeliverResponse()` → `rm.Remove()` 返回 nil | `localSender.DeliverResponse()` 中 `envelope.Release()` | `client/sender_local.go:99` |
| 远程 gRPC 请求 | `MessageBus.call/asyncCall/send` | → `grpcSender.DeliverRequest()` → `defer envelope.Release()` | `grpcSender.DeliverRequest()` defer | `client/sender_remote_grpc.go:105` |
| 远程 gRPC 回复 | handler | → `grpcSender.DeliverResponse()` → `defer envelope.Release()` | `grpcSender.DeliverResponse()` defer | `client/sender_remote_grpc.go:110` |

### 关键不变量

1. **PostJob 转移所有权**：`PostJob(job)` 后调用方不得再持有 job，无论成功或失败。
2. **成功路径**：Worker `safeExec()` → 业务执行 → `ExecuteOnComplete()` → `job.Release()`。
3. **失败路径**（Suspended / Reject / Dispatch 失败）：`OnJobDiscarded()` → `ExecuteOnComplete()` → `job.Release()`。
4. **远程 sender**：`defer envelope.Release()` 在 send 完成后立即释放。
5. **Call 失败后 envelope 双重释放风险**：`MessageBus.call()` 在 `DeliverRequest` 失败后会调用 `envelope.Release()`，但 `localSender.DeliverRequest` 内部通过 PostJob 转移了所有权——PostJob 失败时 Mailbox 已内化释放。因此 bus.go 的 `envelope.Release()` 仅在 localSender 本身返回 err（未进入 PostJob）时才需要。✅ 当前实现正确：`localSender.DeliverRequest` 在 PostJob 失败时直接 return err，不释放 envelope（Mailbox 已释放）。但 bus.go 此时又调了 `envelope.Release()`——**这是一个潜在的双重释放**。

> ⚠️ **发现问题 #1**：`MessageBus.call()` 和 `MessageBus.send()` 在 `DeliverRequest` 失败后调用 `envelope.Release()`，但如果失败路径经过了 `PostJob`，Mailbox 已经释放了 Job（包含 envelope 作为 payload）。不过 Job 释放不等于 envelope 释放（payload 生命周期独立于 Job），需要确认 Mailbox PostJob 失败时是否释放了 payload（envelope）。

---

## 二、Job 所有权路径

| 场景 | 创建者 | 所有权转移 | 最终释放者 | 文件 |
|------|--------|-----------|-----------|------|
| 正常执行 | `localSender` / 业务 | → `PostJob()` → Worker queue → `safeExec()` | `safeExecInternal()` → `job.Release()` (defer) | `worker.go` |
| PostJob Suspended 拒绝 | 同上 | → `PostJob()` → Mailbox 拒绝 | `Mailbox.discardJob()` → `job.Release()` | `mailbox.go` |
| PostJob Middleware Reject | 同上 | → `PostJob()` → 中间件拒绝 | `PostJob()` → `ExecuteOnComplete()` → `job.Release()` | `mailbox.go` |
| PostJob Dispatch 失败 | 同上 | → `PostJob()` → DispatchJob 失败 | `PostJob()` → `ExecuteOnComplete()` → `job.Release()` | `mailbox.go` |
| Stop Drain Execute | Worker 持有 | Worker 主循环退出 → defer drain | `safeExec(job)` 在 drain 中执行 → `job.Release()` | `worker.go` |
| Stop Drain Discard | Worker 持有 | Worker 主循环退出 → defer drain | `discardExec(job)` → `OnJobDiscarded()` → `job.Release()` | `worker.go` |
| Worker SubmitJob 后 Worker 已关闭 | 同上 | → `SubmitJob()` 返回 `ErrMailboxWorkerClosed` | ⚠️ **需验证**：SubmitJob 失败时 Job 未释放。Mailbox.PostJob → DispatchJob → SubmitJob 失败会回到 PostJob 的 dispatch 失败路径释放。✅ 链路完整。 |

### 关键不变量

1. Job 由 `pool.Get()` 创建，通过 `pool.Put()` 回收，`DataRef` CAS 防重复释放。
2. **Job 与 payload 生命周期正交**：Job.Release 不释放 payload。payload 由各自的 Release 机制管理。
3. `safeExecInternal` 中 `defer job.Release()` 保证即使 panic 也释放 Job。

---

## 三、MessageBus 所有权路径

| 场景 | 创建者 | 最终释放者 | 文件 |
|------|--------|-----------|------|
| Call / AsyncCall / Send | `MessageBusFactory.New()` | 方法尾部 `defer ReleaseMessageBus(mb)` | `msgbus/bus.go` |
| CallWithOpt (NotRecycle=true) | 同上 | 调用方手动释放 | `msgbus/bus.go` |
| MultiBus 内部调用 | 同上 | `callInternal(recycle=true)` 尾部释放 | `msgbus/bus.go` |

### 关键不变量

1. MessageBus 由 `PerPPool` 管理（per-P 无锁池），DataRef CAS 防重复释放。
2. **禁止跨 goroutine 传递**（per-P pool 要求使用和归还在同一 P 上）。

---

## 四、CallState 所有权路径

| 场景 | 创建者 | 所有权持有者 | 最终释放者 | 文件 |
|------|--------|-------------|-----------|------|
| 同步 Call | `monitor.NewCallState()` | 调用方（bus.go） | `state.Wait()` 后 `state.Release()` | `msgbus/bus.go:329,335` |
| 同步 Call 失败（DeliverRequest err） | 同上 | 调用方 | `state.Release()` 在 bus.go err 路径 | `msgbus/bus.go:322` |
| AsyncCall 正常回复 | 同上 | RpcMonitor | `state.Complete()` → PostJob callback → `getCallStatePool().Put(s)` | `monitor/call_state.go:175` |
| AsyncCall 超时 | 同上 | RpcMonitor timer | timer 回调 → `state.Complete()` → 同上 | `monitor/monitor.go:270-276` |
| AsyncCall late response（已超时移除） | — | 已释放 | `rm.Remove()` 返回 nil → state 已由 timer 释放 → 无二次操作 ✅ | `client/sender_local.go:77` |
| Monitor 关闭时 | 同上 | RpcMonitor | `rm.Add()` 时检测 `isClosed()` → `state.Complete()` 立即触发 | `monitor/monitor.go:252` |

### 关键不变量

1. CallState 由 `syncPool` 管理，`DataRef` CAS 防重复释放。
2. **同步 Call**：调用方持有 → Wait → 手动 Release。
3. **AsyncCall**：monitor 持有 → Complete() 内 PostJob callback → 自动 Release。
4. **超时**：timer 通过 `rm.remove(seqId)` 取出 state → `Complete()` → Release。
5. **迟到响应**：`rm.Remove()` CAS 返回 nil（timer 已取走），`localSender.DeliverResponse` 只释放 envelope，不触发 state 二次释放 ✅。

---

## 五、发现的问题与风险

### 问题 #1：bus.go Call/Send 失败路径可能双重释放 Envelope

**位置**：`msgbus/bus.go` `call()` 方法 ~L318-322，`send()` 方法 ~L571

**现状**：
```go
if err := mb.receiver.DeliverRequest(newCtx, envelope); err != nil {
    _ = mt.Remove(reqId)
    state.Release()
    envelope.Release()  // ← 如果 DeliverRequest 内部已经 PostJob 释放了呢？
    ...
}
```

**分析**：
- `localSender.DeliverRequest` 创建 RpcJob 把 envelope 作为 payload 放入，然后 `PostJob(rpcJob)`。
- 如果 PostJob 失败（Suspended/Reject/Dispatch失败），Mailbox 释放 rpcJob（但不释放 payload/envelope）。
- 因此 envelope 在 PostJob 失败时**未被释放**，bus.go 的 `envelope.Release()` 是正确的 ✅。
- 但如果 `localSender.DeliverRequest` 在 PostJob 之前就返回 err（如 `lc.IsClosed()`），envelope 同样未被释放，bus.go 的 Release 也是正确的 ✅。

**结论**：✅ 当前实现正确。Job 与 payload 生命周期正交，PostJob 失败释放 Job 但不释放 envelope。bus.go 的 envelope.Release() 在所有失败路径上都是必要的。

### 问题 #2：localSender.DeliverResponse 异步回调路径 state 泄漏 ✅ 已修复

**位置**：`client/sender_local.go` `DeliverResponse()` ~L75-88

**修复内容**：
1. 异步回调路径 PostJob 成功后补充 `state.Release()` 释放 CallState。
2. 异步回调路径 PostJob 失败后补充 `envelope.Release()` + `state.Release()` 释放 envelope 和 CallState。

### 问题 #3：localSender.DeliverResponse PostJob 失败时 envelope 泄漏 ✅ 已修复

**位置**：`client/sender_local.go:88`

**修复内容**：同问题 #2，PostJob 失败时补充 `envelope.Release()` 和 `state.Release()`。

### 问题 #4：Monitor 超时回调 defer 读取已释放 state.ctx ✅ 已修复

**位置**：`monitor/monitor.go` `Add()` 方法中 timer 回调闭包

**现状**：timer 超时回调中，`defer` 在 `st.Complete()` 之后读取 `state.ctx`。但 `Complete()` 后同步 Call 的调用方可能立即 `Release() → Reset()`，导致 `state.ctx` 被清空——形成 data race。

**修复内容**：将日志输出从 `defer`（Complete 之后）移到 `Complete()` 之前执行，确保读取 `state.ctx` 时 state 仍被当前 goroutine 独占。

---

## 六、P0-1 测试计划

基于审计结果，需要以下测试覆盖：

| 测试编号 | 场景 | 验证点 | 优先级 |
|---------|------|--------|--------|
| T-1.1 | Job.Release 释放后 DataRef 变为 unref | CAS 防重复释放 | P0 |
| T-1.2 | Job.Release 重复调用不 panic | 幂等性 | P0 |
| T-1.3 | PostJob 成功 → Worker 执行 → Job.Release | 正常路径释放 | P0 |
| T-1.4 | PostJob Suspended → discardJob → Release | 拒绝路径释放 | P0 |
| T-1.5 | PostJob Middleware Reject → Release | 中间件拒绝路径 | P0 |
| T-1.6 | PostJob Dispatch 失败 → Release | dispatch 失败路径 | P0 |
| T-1.7 | Stop DrainDiscard → discardExec → Release | 停止时丢弃 | P0 |
| T-1.8 | Stop DrainExecute → safeExec → Release | 停止时执行 | P0 |
| T-1.9 | Call 成功 → state.Release | 同步调用正常释放 | P0 |
| T-1.10 | Call DeliverRequest 失败 → state.Release + envelope.Release | 失败路径 | P0 |
| T-1.11 | AsyncCall 超时 → timer 触发 → state.Complete → Release | 超时路径 | P0 |
| T-1.12 | late response → rm.Remove nil → 只释放 envelope | 迟到响应 | P0 |
| T-1.13 | localSender.DeliverResponse 异步回调 → state Release | 问题 #2 验证 | P0 |

---

## 七、已确认的安全点

1. ✅ `DataRef` CAS 防重复释放：所有 pool 对象（Job/Envelope/CallState/MessageBus）都内嵌 `dto.DataRef`。
2. ✅ PostJob 所有权转移协议文档完善（`IMailBox.go` 和 `mailbox.go` 注释）。
3. ✅ Worker `safeExecInternal` 使用 defer 保证 panic 时也释放 Job。
4. ✅ 远程 sender 使用 `defer envelope.Release()` 保证网络错误时也释放。
5. ✅ RpcMonitor 超时 timer 和响应到达使用 `remove` CAS 保证不重复处理。
6. ✅ Job 与 payload 生命周期正交，不存在级联释放依赖。

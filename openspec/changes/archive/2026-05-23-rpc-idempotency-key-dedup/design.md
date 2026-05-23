# RPC Idempotency Key Dedup Design

## Context

当前 RPC 请求里的 `ReqId` 由 `RpcMonitor.GenSeq()` 生成，并用于 Call/AsyncCall 等待态、回复匹配、超时回调和取消句柄。远端 Handler 目前仍把 `ReqId` 当作去重依据，这会把框架 correlation id 与业务幂等键混在一起：`ReqId` 每次请求都应该不同，而业务幂等键在同一业务动作重试时应该保持一致。

现有去重器接口 `Seen(serviceUid string, id uint64)` 也绑定了 `senderServiceUid + reqId` 的旧模型。新的机制需要把幂等键作为完整 key 传递给去重器，而不是由 Handler 使用 `ReqId` 推导。

## Goals / Non-Goals

**Goals:**

- 将 `ReqId` 明确为框架 correlation id，只服务于 monitor、回复关联、取消和观测。
- 增加显式 `IdempotencyKey`，由调用方或业务封装层提供。
- Handler 仅在请求携带 `IdempotencyKey` 时执行幂等去重。
- `CallWithOpt`、`AsyncCallWithOpt`、`SendWithOpt` 均可通过 option 传递幂等键。
- `Send` 需要携带 `SenderPid` 用于身份、授权和审计，但不需要生成或携带 `ReqId`。
- `ReqId` 生成规则可以简化为节点内单调自增，不再需要为了跨重启去重构造时间戳高位。

**Non-Goals:**

- 不规定业务如何生成 `IdempotencyKey`。
- 不在框架层反射请求参数或自动 hash 请求体。
- 不改变 `ReqId` 在 monitor、reply、cancel 路径中的用途。
- 不要求未携带 `IdempotencyKey` 的请求被去重。

## Decisions

1. **新增协议字段 `IdempotencyKey`。**

   `actor.Message` 增加字符串字段，例如 `string IdempotencyKey = 15`。该字段表达完整业务幂等键，框架不再自动拼接 sender、receiver、method 或 reqId。

   备选方案是通过 `ContextHeaders` 传递幂等键。该方案不需要改 proto，但幂等是 RPC 协议语义，不是普通 metadata；放在显式字段中更易测试、文档化和跨语言兼容。

2. **调用侧显式传入幂等键。**

   `dto.BusOption` 增加 `IdempotencyKey string`，并提供 `WithIdempotencyKey(key string)`。业务可自行使用请求中的稳定字段生成 key，例如 `order:create:<clientOrderNo>`。框架只负责传递该 key，不负责决定 `Send`、`Call` 或 `AsyncCall` 是否必须设置该 key。

   框架不提供自动从请求参数生成 key 的默认行为，因为框架无法判断哪些字段代表业务唯一性，也无法保证不同版本请求结构序列化稳定。

3. **去重器支持字符串 key。**

   扩展 `IDeDuplicator`，增加 `SeenKey(key string) bool`。TTL/LRU 实现直接以完整 key 存储。旧的 `Seen(serviceUid, id)` 可暂时保留作为兼容接口，但 Handler 新逻辑不再使用它。

4. **Handler 使用 `IdempotencyKey`，不再使用 `ReqId` 做业务去重。**

   远端 Handler 在 decode payload 前读取 `req.IdempotencyKey`。若为空，跳过去重；若非空，调用 `dedup.SeenKey(req.IdempotencyKey)`，已存在则直接返回 nil。

   这个逻辑不把 `SenderPid` 拼入去重 key。调用方若希望按 sender 隔离 key，应把 sender 或业务命名空间编码到 `IdempotencyKey` 里。`SenderPid` 仍作为请求身份字段存在，供授权和审计路径使用。

5. **`Send` 不生成 `ReqId`。**

   `Send` 是 fire-and-forget 语义，不进入 monitor 等待态，也不需要 reply correlation。因此 `Send` 请求应携带 `SenderPid`，但 `ReqId` 可以保持零值。若业务需要幂等去重，应显式通过 `WithIdempotencyKey` 设置业务 key。

6. **`ReqId` 生成规则回归简单自增。**

   因为 `ReqId` 不再承担跨重启去重语义，`RpcMonitor.GenSeq()` 可以只使用节点内 `atomic.AddUint64` 自增值。唯一性只需满足当前 Node 运行期间 monitor map 的关联需求，主要用于 Call/AsyncCall 的等待态和回复匹配。

## Risks / Trade-offs

- **幂等键冲突导致误去重** → 调用方必须提供完整业务命名空间，例如 `payment:charge:<orderId>`，不要传裸 ID。
- **未传幂等键的请求不再去重** → 这是显式语义；需要幂等的业务必须通过 option 传入 key。
- **协议字段变更需要重新生成 protobuf** → 实施时必须同步更新生成代码，并跑 RPC 相关测试。
- **旧代码依赖 ReqId 去重** → 迁移期可保留 `Seen(serviceUid, id)` 接口，但 Handler 不再走旧逻辑。
- **Send 身份与幂等 key 可能被混淆** → 明确 `SenderPid` 是身份字段，`IdempotencyKey` 是业务幂等字段，两者互不替代。

## Migration Plan

1. 扩展 proto 和生成代码，新增 `IdempotencyKey` 字段。
2. 扩展 `Meta`、`IEnvelopeMeta`、`BusOption` 和 option builder。
3. 在 MessageBus 创建请求时传递幂等键，并确保 Send 携带 SenderPid 但不生成 ReqId。
4. 扩展去重器字符串 key 接口。
5. 修改 Handler 使用 `IdempotencyKey` 去重。
6. 简化 `RpcMonitor.GenSeq()` 为自增序列。
7. 更新测试，覆盖有/无幂等键、重复幂等键、Call/AsyncCall/Send 三类请求。

## Open Questions

- 是否需要提供框架辅助函数 `WithIdempotencyKeyHash(parts ...string)`，还是只提供原始 `WithIdempotencyKey`。
- 是否要在日志中输出命中的 `IdempotencyKey`，以及是否需要脱敏或截断。

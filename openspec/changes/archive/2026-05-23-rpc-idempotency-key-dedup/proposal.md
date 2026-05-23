# RPC Idempotency Key Dedup Proposal

## Why

当前 RPC 远端去重复用了 `ReqId`，但 `ReqId` 实际承担的是 `RpcMonitor` 等待态关联、回复匹配、取消句柄和请求观测标识，不适合作为业务幂等键。为避免把 correlation id、回复路径和业务幂等语义混在一起，需要引入独立的幂等键机制。

## What Changes

- 新增 RPC 请求级幂等键 `IdempotencyKey`，由调用方或业务封装层提供。
- 新增 `CallWithOpt` / `AsyncCallWithOpt` / `SendWithOpt` 可使用的 option，例如 `WithIdempotencyKey`。
- 远端 Handler 不再使用 `ReqId` 判断业务幂等；只有请求携带非空 `IdempotencyKey` 时才进入去重。
- `ReqId` 回归框架内部 Call/AsyncCall correlation id 语义，可使用简单的节点内自增序列生成，不再为了去重语义计算；Send 不需要生成 `ReqId`。
- Send 仍需要携带 `SenderPid` 作为发送方身份，幂等键是否携带由业务通过 option 自行控制。
- 去重器从 `senderServiceUid + reqId` 模型演进为按完整 `IdempotencyKey` 查重。
- 不规定幂等键如何生成；业务可直接传入，也可用请求中的稳定业务字段派生。

## Capabilities

### New Capabilities

- `rpc-idempotency`: RPC 请求幂等键传递、远端去重和幂等语义边界。

### Modified Capabilities

- `rpc`: 非 Reply 请求的去重语义从 `ReqId` 去重改为 `IdempotencyKey` 去重。

## Impact

- 协议：`actor.Message` 需要新增 `IdempotencyKey` 字段。
- API：`dto.BusOption` 需要新增幂等键字段及 builder。
- 消息总线：`MessageBus` 创建 envelope 时需要把 option 中的幂等键写入 meta/proto。
- 远端 Handler：去重逻辑改为读取 `IdempotencyKey`，不再用 `ReqId` 触发去重。
- 去重器：需要支持字符串 key 的 `SeenKey` 语义，并保留或迁移旧接口。
- 测试：需要覆盖 Call、AsyncCall、Send 携带幂等键时的去重行为，以及未携带幂等键时不去重。

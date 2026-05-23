# RPC Idempotency 规范

## 目的

本规范描述 RPC 请求显式幂等键的传递、远端去重语义，以及 `ReqId` 与业务幂等键之间的职责边界。

## Requirements

### Requirement: RPC 请求必须支持显式幂等键

RPC 调用方必须能够通过调用选项为请求设置完整的业务幂等键。框架不得自动从请求参数推导幂等键。

#### Scenario: 调用方通过 option 设置幂等键

- **WHEN** 调用方使用 `WithIdempotencyKey` 构建 RPC 请求
- **THEN** 系统必须把该幂等键写入请求消息

#### Scenario: 未设置幂等键时请求不启用幂等去重

- **WHEN** 调用方未设置幂等键
- **THEN** 系统不得对该请求执行业务幂等去重

### Requirement: RPC 远端 Handler 必须按幂等键去重

远端 Handler 必须仅使用请求携带的 `IdempotencyKey` 执行业务幂等去重，不得使用 `ReqId` 作为业务幂等依据。

#### Scenario: 首次收到幂等键时正常投递

- **WHEN** Handler 收到携带非空 `IdempotencyKey` 且该 key 未出现过的请求
- **THEN** Handler 必须记录该 key
- **AND** 必须继续投递请求到目标 Dispatcher

#### Scenario: 重复收到相同幂等键时丢弃重复请求

- **WHEN** Handler 收到携带非空 `IdempotencyKey` 且该 key 已存在的请求
- **THEN** Handler 必须跳过目标 Dispatcher 投递
- **AND** 必须返回 nil

#### Scenario: 幂等键去重不隐式拼接 SenderPid

- **WHEN** Handler 收到携带非空 `IdempotencyKey` 的请求
- **THEN** Handler 必须仅使用完整 `IdempotencyKey` 判断是否重复
- **AND** 不得自动把 `SenderPid` 拼接到去重 key 中

### Requirement: 幂等键必须由调用方承担命名空间语义

框架必须把 `IdempotencyKey` 当作完整 key 使用，不得自动拼接 sender、receiver、method 或 reqId。

#### Scenario: 两个请求携带完全相同的幂等键

- **WHEN** 两个请求携带完全相同的 `IdempotencyKey`
- **THEN** 系统必须把第二个请求视为重复请求

#### Scenario: 两个请求携带不同的幂等键

- **WHEN** 两个请求携带不同的 `IdempotencyKey`
- **THEN** 系统必须把它们视为不同请求

### Requirement: ReqId 必须保留为框架 correlation id

`ReqId` 必须继续用于 monitor 等待态、回复匹配和取消，不得再作为业务幂等键。无需回复的 Send 请求不得为了幂等或观测额外生成 `ReqId`。

#### Scenario: Call 请求生成 ReqId 后注册 monitor 等待态

- **WHEN** 调用方发起需要响应的 Call 请求
- **THEN** 系统必须生成 `ReqId`
- **AND** 必须使用同一个 `ReqId` 注册 monitor 等待态和构造请求消息

#### Scenario: Send 请求不生成 ReqId

- **WHEN** 调用方发起 fire-and-forget Send 请求
- **THEN** 系统必须携带 `SenderPid` 作为发送方身份
- **AND** 不得为该请求生成或携带 `ReqId`
- **AND** 是否携带 `IdempotencyKey` 必须由业务通过 option 自行控制

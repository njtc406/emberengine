# RPC 发送与远端入口规范

## 目的

本规范描述 `engine/pkg/rpc/` 当前代码已经实现的发送端管理、Dispatcher 行为以及远端消息入口 Handler 的语义。

## Requirements

### Requirement: SenderManager 必须按协议管理 sender 创建器与 sender 实例缓存

#### Scenario: 默认创建器按 rpc type 注册

- **GIVEN** 系统创建 `SenderManager`
- **WHEN** 初始化默认 sender map
- **THEN** 必须为 rpcx、grpc 和 nats 注册默认创建器

#### Scenario: 同一 addr 和 rpc type 复用同一个 sender 实例

- **GIVEN** 某个远端地址和 rpc type 已创建 sender
- **WHEN** 再次获取同一地址和类型的 sender
- **THEN** SenderManager 必须复用已缓存的 sender 实例

### Requirement: Dispatcher 必须按是否本地 mailbox 选择发送路径

#### Scenario: Dispatcher 持有本地 mailbox 时走本地 sender

- **GIVEN** Dispatcher 已绑定本地 `IMailboxChannel`
- **WHEN** 调用 `DeliverRequest` 或 `DeliverResponse`
- **THEN** 必须使用本地 sender 路径

#### Scenario: Dispatcher 没有本地 mailbox 时按 PID 地址和 rpc type 选择远端 sender

- **GIVEN** Dispatcher 没有本地 mailbox
- **WHEN** 调用 `DeliverRequest` 或 `DeliverResponse`
- **THEN** 必须根据 `pid.Address` 和 `pid.RpcType` 获取远端 sender

### Requirement: 远端 Handler 必须先同步 PID 主从标志，再处理请求

#### Scenario: RpcMessageHandler 在反序列化后同步 SenderPid 和 ReceiverPid 的 MasterFlag

- **GIVEN** 一个远端请求已被反序列化成 `actor.Message`
- **WHEN** `RpcMessageHandler` 开始处理
- **THEN** 若 SenderPid 或 ReceiverPid 存在，必须先调用 `SyncMasterFlag()`

### Requirement: Reply 消息必须通过 RpcMonitor 查找等待态并完成回调

#### Scenario: Reply 到达时移除等待态并完成 CallState

- **GIVEN** 一个 `Reply=true` 的远端消息到达
- **WHEN** `RpcMessageHandler` 处理该消息
- **THEN** Handler 必须从 RpcMonitor 中移除对应状态
- **AND** 必须把响应或错误写入状态并调用 `Complete()`

### Requirement: 非 Reply 请求必须支持去重与授权检查

#### Scenario: 携带幂等键的请求走幂等去重检查

- **GIVEN** 一个非 Reply 请求携带非空 `IdempotencyKey`
- **WHEN** Handler 处理该请求
- **THEN** 必须使用去重器按完整 `IdempotencyKey` 做去重
- **AND** 不得使用 `ReqId` 作为业务幂等键

#### Scenario: 未携带幂等键的请求跳过幂等去重

- **GIVEN** 一个非 Reply 请求未携带 `IdempotencyKey`
- **WHEN** Handler 处理该请求
- **THEN** 必须跳过业务幂等去重

#### Scenario: 授权器启用时在 decode 前做授权检查

- **GIVEN** Handler 已注入且启用了 Authorizer
- **WHEN** 处理普通请求
- **THEN** 必须在 payload decode 之前执行授权检查
- **AND** 未授权请求必须被拒绝

### Requirement: 普通请求必须被封装为 Envelope 后投递给目标 Dispatcher

#### Scenario: Handler 从 actor.Message 构建 MsgEnvelope 并调用目标 Dispatcher

- **GIVEN** 一个普通远端请求已通过去重与授权检查
- **WHEN** Handler 继续处理该请求
- **THEN** 必须构建 MsgEnvelope
- **AND** 必须把 method、request、needResponse、reqId、deadline、sender/receiver 信息填入 envelope
- **AND** 必须通过 receiver 对应的 Dispatcher 投递请求

## Out of Scope

- gRPC、NATS、rpcx 各自的底层传输细节
- MessageBus 与 Envelope 的完整内部编码实现

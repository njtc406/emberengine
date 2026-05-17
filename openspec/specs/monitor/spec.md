# Monitor RPC 监视规范

## 目的

本规范描述 `engine/pkg/monitor/` 当前代码已经实现的 RPC 调用等待态、超时回调和监视器关闭语义。

## Requirements

### Requirement: CallState 必须作为一次 RPC 调用的等待载体

#### Scenario: 同步调用通过 done channel 等待完成

- **GIVEN** 一个同步 RPC 调用创建了 `CallState`
- **WHEN** 调用方执行 `Wait()`
- **THEN** 调用方必须等待 `done` 通道收到完成信号

#### Scenario: 异步调用通过回调完成后自动释放 CallState

- **GIVEN** 一个异步 RPC 调用持有回调函数
- **WHEN** `CallState.Complete()` 被触发
- **THEN** 系统必须把回调投递到 dispatcher 的 mailbox
- **AND** 必须在回调投递路径后释放 CallState

### Requirement: RpcMonitor 必须为 pending 调用分片存储等待状态

#### Scenario: Add 把 CallState 放入分桶等待表

- **GIVEN** 一个新创建的 `CallState`
- **WHEN** 调用 `RpcMonitor.Add`
- **THEN** 系统必须根据 reqId 把它写入对应 wait bucket

### Requirement: RpcMonitor 必须通过时间轮调度超时回调

#### Scenario: 调用超时后返回 ErrRPCCallTimeout

- **GIVEN** 一个 CallState 已注册超时定时器
- **WHEN** 定时器触发且该调用仍未完成
- **THEN** 系统必须把结果设置为 `ErrRPCCallTimeout`
- **AND** 必须触发 `Complete()`

### Requirement: Stop 必须释放所有未完成的等待态

#### Scenario: 关闭监视器时清空所有 buckets

- **GIVEN** RpcMonitor 正在停止
- **WHEN** 调用 `Stop()`
- **THEN** 系统必须停止调度器
- **AND** 必须等待监听 goroutine 退出
- **AND** 必须清空所有 wait buckets 并释放剩余 CallState

## Out of Scope

- RPC 具体编码协议
- 远端传输实现细节

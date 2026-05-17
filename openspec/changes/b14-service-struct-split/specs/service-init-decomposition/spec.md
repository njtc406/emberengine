## ADDED Requirements

### Requirement: Init 必须拆分为独立的子初始化方法

`Service.Init()` 必须将组件创建逻辑拆分为独立的私有子方法，主 Init 方法仅做编排调度。每个子方法负责一个职责域的初始化。

#### Scenario: Init 编排方法依次调用子初始化

- **WHEN** `Service.Init()` 被调用
- **THEN** 必须依次调用 `initLogger`、`initTimers`、`initMailbox`、`initEvents`、`initConcurrent`、`initPID`、`initRPC` 子方法
- **AND** 任一子方法返回 error 时，Init 必须立即返回该 error 并触发回滚

#### Scenario: 子初始化方法可独立执行

- **WHEN** 构造一个最小的 Service 实例并设置必要的前置条件
- **THEN** 每个子初始化方法（如 `initLogger`、`initMailbox`）必须能独立调用并返回正确结果或明确错误
- **AND** 不依赖其他子初始化方法的副作用（除文档化的前置条件外）

### Requirement: 子初始化方法必须位于独立文件

所有 `init*` 子方法必须定义在 `engine/pkg/core/service_init.go` 文件中，与 `service.go` 中的生命周期方法（Start/Stop/PostJob）分离。

#### Scenario: service_init.go 包含所有子初始化方法

- **WHEN** 查看 `engine/pkg/core/service_init.go`
- **THEN** 该文件必须包含 `initLogger`、`initTimers`、`initMailbox`、`initEvents`、`initConcurrent`、`initPID`、`initRPC` 方法定义
- **AND** 该文件不包含 Start/Stop/PostJob 等生命周期方法

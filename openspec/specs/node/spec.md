# Node 运行时规范

## 目的

本规范描述 `engine/pkg/node/` 当前代码已经实现的 Node 自包含运行时模型。

## Requirements

### Requirement: Node 必须持有自包含的运行时组件实例

当前 Node 模型要求配置、日志、协程池、时间轮、去重器、事件总线、集群、服务管理器和 RPC 层组件均由 Node 持有，而不是通过全局单例访问。

#### Scenario: Node 作为 INodeContext 暴露运行时依赖

- **GIVEN** 上层组件依赖 `INodeContext`
- **WHEN** 它们访问配置、日志、时间轮、事件总线、Router 或 EndpointManager
- **THEN** 这些对象必须由当前 Node 实例提供

### Requirement: Node.Start 必须按依赖顺序初始化组件

#### Scenario: 配置必须先于其它组件初始化

- **GIVEN** Node 开始启动
- **WHEN** `Node.Start` 执行
- **THEN** 它必须先加载配置
- **AND** 再初始化日志
- **AND** 再初始化基础设施与上层运行时组件

### Requirement: Node 必须维护可逆序回滚的清理栈

#### Scenario: 启动失败时逆序执行已注册清理步骤

- **GIVEN** Node 启动过程中某个步骤失败
- **WHEN** `Node.Start` 返回错误
- **THEN** Node 必须按逆序执行已注册的 cleanup
- **AND** 不得残留已初始化但未托管的资源

### Requirement: Node 必须支持停止时的逆序清理

#### Scenario: Stop 依赖 stopCleanups 执行逆序收尾

- **GIVEN** Node 已成功启动
- **WHEN** Node 停止
- **THEN** Node 必须使用 stopCleanups 按逆序执行关闭逻辑

## Out of Scope

- 单个子系统内部的全部细节
- 业务服务的具体启动顺序定义

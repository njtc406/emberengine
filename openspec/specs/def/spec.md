# Def 常量与类型规范

## 目的

本规范描述 `engine/pkg/def/` 在当前项目中的角色：它是跨模块共享的基础常量、枚举、错误码和协议级默认值定义层。

## Requirements

### Requirement: def 必须作为跨模块共享的基础定义层

`engine/pkg/def/` 必须只承载被多个模块共同依赖的基础定义，而不承载运行时状态。

#### Scenario: 运行时模块通过 def 共享公共常量

- **GIVEN** config、mailbox、core、event、rpc 等模块都需要默认值或共享枚举
- **WHEN** 这些模块引用基础定义
- **THEN** 它们必须通过 `engine/pkg/def/` 获取共享常量、优先级、错误码或枚举值

### Requirement: def 必须定义 Mailbox、事件、RPC 等公共枚举语义

#### Scenario: Mailbox Job 类型通过 def 统一表示

- **GIVEN** Job、Mailbox、Service handler 等多个层都需要识别作业类型
- **WHEN** 它们读写 Job 类型
- **THEN** 必须使用 `def.MailboxJobType` 及其相关常量统一表达

#### Scenario: 优先级通过 def 统一表示

- **GIVEN** Mailbox、SuspendPolicy、中间件和 Event 都需要识别优先级
- **WHEN** 它们比较或写入优先级
- **THEN** 必须使用 `def.Priority` 及其相关常量统一表达

### Requirement: def 必须为配置系统提供默认值常量

#### Scenario: Config 默认值来自 def

- **GIVEN** 配置文件未提供某些基础设施参数
- **WHEN** config 模块设置默认值
- **THEN** 必须使用 `def` 中声明的默认路径、默认池大小、默认 bucket 大小等常量

## Out of Scope

- 任一运行时模块的具体生命周期逻辑
- 业务层面的领域模型

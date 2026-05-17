# Log 日志规范

## 目的

本规范描述 `engine/pkg/log/` 当前代码已经实现的日志构造、默认值、输出路由与关闭语义。

## Requirements

### Requirement: LoggerConf 必须在构造前被标准化

#### Scenario: fixConf 为日志配置补齐默认值并归一化输出格式

- **GIVEN** 调用方传入一个 `LoggerConf`
- **WHEN** 系统准备创建 Logger
- **THEN** 必须先执行配置标准化
- **AND** 必须补齐默认输出格式、默认日志级别、默认切割配置和默认路由

#### Scenario: JSON 输出必须禁用颜色

- **GIVEN** `OutputFormat=json`
- **WHEN** 系统标准化配置
- **THEN** 必须强制关闭颜色输出

### Requirement: 未配置 PrefixName 时不得生成文件路由

#### Scenario: PrefixName 为空时清空文件路由

- **GIVEN** `LoggerConf.PrefixName` 为空
- **WHEN** 系统标准化配置
- **THEN** 必须清空路由配置
- **AND** 不得为文件输出构造 writer 路由

### Requirement: NewDefaultLogger 必须返回可关闭的 zap 封装 Logger

#### Scenario: 创建日志对象时构建 zap core 并返回 Logger

- **GIVEN** 一份合法的 `LoggerConf`
- **WHEN** 调用 `NewDefaultLogger`
- **THEN** 系统必须构造 zap core
- **AND** 必须返回实现 `ILoggerX` 的 `Logger`

### Requirement: Logger.Close 必须幂等地关闭底层资源

#### Scenario: 多次关闭 Logger 只执行一次真实关闭动作

- **GIVEN** 一个已创建的 Logger
- **WHEN** `Close()` 被多次调用
- **THEN** 只有第一次调用可以真正执行 `Sync` 和 closers

### Requirement: WithContext 必须把 ember context headers 注入日志字段

#### Scenario: context 中存在 header 时生成带字段的新 Logger

- **GIVEN** 一个包含 headers 的 context
- **WHEN** 调用 `Logger.WithContext(ctx)`
- **THEN** 返回的新 Logger 必须附带这些 header 字段

## Out of Scope

- 具体日志采集系统的部署方式
- 各业务服务的日志字段约定

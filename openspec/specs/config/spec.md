# Config 配置规范

## 目的

本规范描述 `engine/pkg/config/` 当前代码已经实现的配置装载、默认值、校验与远程配置行为。

## Requirements

### Requirement: Config 必须是 Node 级实例，而不是全局单例

`config.Config` 必须作为 Node 持有的实例化配置对象存在，每个 Node 使用独立的 viper 和配置状态。

#### Scenario: NewConfig 返回独立配置实例

- **GIVEN** 调用方需要创建节点配置
- **WHEN** 调用 `config.NewConfig()`
- **THEN** 必须返回一个新的 `Config` 实例
- **AND** 该实例必须持有独立的 runtime viper、cluster viper 和内部配置表

### Requirement: Load 必须完成本地配置解析与目录初始化

`Config.Load(confPath)` 必须按固定顺序完成本地配置文件解析、必要目录初始化和最终校验。

#### Scenario: Load 从 node.yaml 和环境变量构建配置

- **GIVEN** 一个配置目录
- **WHEN** 调用 `Config.Load(confPath)`
- **THEN** 系统必须读取 `node.yaml`
- **AND** 必须使用环境变量替换 `${VAR}`
- **AND** 必须将结果反序列化到 `Config`

#### Scenario: Load 必须优先允许环境变量覆盖配置路径

- **GIVEN** 设置了 `EMBER_CONF_PATH`
- **WHEN** 调用 `Config.Load(confPath)`
- **THEN** 系统必须优先使用 `EMBER_CONF_PATH` 指向的目录作为配置目录

#### Scenario: Load 必须创建必要目录

- **GIVEN** 配置已解析出 PVPath 和日志目录
- **WHEN** `Config.Load` 完成解析后进入目录初始化
- **THEN** 系统必须创建 PVPath
- **AND** 必须创建系统日志目录

### Requirement: Load 必须在关键顶层配置缺失时失败

当前代码要求 `NodeConf`、`ServiceConf`、`SystemLogger` 必须存在。

#### Scenario: 顶层配置缺失时返回错误

- **GIVEN** 配置文件缺少 `NodeConf`、`ServiceConf` 或 `SystemLogger`
- **WHEN** 调用 `Config.Load`
- **THEN** 系统必须返回错误
- **AND** 不得继续进入后续字段访问流程

### Requirement: 配置系统必须支持本地模式和远程服务配置模式

服务配置解析必须根据 `ServiceConf.OpenRemote` 选择本地文件或远程配置源。

#### Scenario: 本地模式下从本地配置文件读取服务配置

- **GIVEN** `ServiceConf.OpenRemote=false`
- **WHEN** 系统解析服务配置
- **THEN** 必须从本地配置目录读取服务配置文件

#### Scenario: 远程模式下从远程配置源读取启动服务和服务配置

- **GIVEN** `ServiceConf.OpenRemote=true`
- **WHEN** 系统解析启动服务列表和服务配置
- **THEN** 必须通过远程配置提供者读取配置
- **AND** 必须支持监听远程配置变化

### Requirement: 配置系统必须在完成装载前应用默认值

配置系统必须为关键字段提供默认值，以保证未显式填写时仍具备可运行的基础行为。

#### Scenario: 未显式配置时回退到 Node 默认值

- **GIVEN** 配置文件未完整提供 Node 基础设施参数
- **WHEN** 系统执行默认值设置
- **THEN** 必须为 `NodeConf`、`RpcMonitorConf`、`TimingWheelConf` 等字段补齐默认值

#### Scenario: 未显式配置时回退到 Service 初始化默认值

- **GIVEN** 某个服务初始化配置缺少 timer、event channel 或 rpc type
- **WHEN** 系统整理 `ServiceInitConf`
- **THEN** 必须补齐默认 timer 配置、默认事件通道大小和默认 rpc type

### Requirement: 配置系统必须在最终返回前执行结构体验证

#### Scenario: 校验失败时 Load 返回错误

- **GIVEN** 配置内容不满足 binding 约束或结构体校验规则
- **WHEN** 调用 `Config.Load`
- **THEN** 系统必须返回验证错误
- **AND** 不得返回一个伪成功的 Config 实例

## Out of Scope

- 具体业务服务配置项的业务语义
- 远程配置变更后的业务热更新时序

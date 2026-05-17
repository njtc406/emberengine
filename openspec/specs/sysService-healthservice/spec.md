# SysService HealthService 运维端点规范

## 目的

本规范描述 `engine/pkg/sysService/healthservice/` 当前代码已经实现的运维 HTTP 端点与生命周期行为。

## Requirements

### Requirement: RegisterHealthService 必须注册服务工厂和配置定义

#### Scenario: 注册阶段同时写入 services 与 config 注册表

- **GIVEN** 系统加载 healthservice 包
- **WHEN** 调用 `RegisterHealthService()`
- **THEN** 必须注册 `HealthService` 服务工厂
- **AND** 必须注册 health 配置定义

### Requirement: HealthService 必须暴露 /health、/ready、/metrics 三个端点

#### Scenario: OnInit 构造 HTTP mux 并挂载三个处理器

- **GIVEN** HealthService 正在初始化
- **WHEN** 调用 `OnInit()`
- **THEN** 必须创建 `http.ServeMux`
- **AND** 必须挂载 `/health`、`/ready`、`/metrics`

### Requirement: /ready 必须反映 NodeContext 的就绪状态

#### Scenario: Node 未 ready 时返回 503

- **GIVEN** HealthService 当前没有 NodeContext 或 `ctx.IsReady()` 为 false
- **WHEN** 请求 `/ready`
- **THEN** 必须返回 503 和 `not ready`

#### Scenario: Node ready 时返回 200

- **GIVEN** `ctx.IsReady()` 为 true
- **WHEN** 请求 `/ready`
- **THEN** 必须返回 200 和 `ready`

### Requirement: /metrics 必须返回 NodeContext 当前聚合的指标文本

#### Scenario: NodeContext 可用时输出 runtime metrics text

- **GIVEN** HealthService 可以获取 NodeContext
- **WHEN** 请求 `/metrics`
- **THEN** 必须返回 `GetRuntimeMetricsText()` 生成的文本

### Requirement: OnRelease 必须幂等关闭 HTTP Server

#### Scenario: 多次释放只执行一次 Shutdown

- **GIVEN** HealthService 已经启动
- **WHEN** `OnRelease()` 被多次调用
- **THEN** 只有第一次调用可以真正执行 server shutdown

## Out of Scope

- 认证鉴权
- 更复杂的运维探针聚合逻辑

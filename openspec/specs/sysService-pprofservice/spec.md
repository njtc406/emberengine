# SysService PprofService 性能剖析端点规范

## 目的

本规范描述 `engine/pkg/sysService/pprofservice/` 当前代码已经实现的 pprof HTTP 暴露语义。

## Requirements

### Requirement: RegisterPprofService 必须注册服务工厂和配置定义

#### Scenario: 注册阶段同时写入 services 与 config 注册表

- **GIVEN** 系统加载 pprofservice 包
- **WHEN** 调用 `RegisterPprofService()`
- **THEN** 必须注册 `PprofService` 服务工厂
- **AND** 必须注册 pprof 配置定义

### Requirement: OnInit 必须创建 HttpModule 并挂载 pprof 路由

#### Scenario: 根据运行状态创建 HttpModule

- **GIVEN** PprofService 正在初始化
- **WHEN** 调用 `OnInit()`
- **THEN** 必须根据 Node 当前状态构造 `HttpModule`
- **AND** 必须把 pprof 路由注册进去

### Requirement: PprofService 必须暴露 /debug/pprof 路由

#### Scenario: routerHandler 使用 http.DefaultServeMux 暴露 pprof

- **GIVEN** HttpModule 路由已初始化
- **WHEN** 请求 `/debug/pprof` 或 `/debug/pprof/*pprof`
- **THEN** 请求必须转交给 `http.DefaultServeMux`

### Requirement: OnRelease 必须释放所有子模块

#### Scenario: 停止 PprofService 时释放 HttpModule

- **GIVEN** PprofService 已初始化子模块
- **WHEN** 调用 `OnRelease()`
- **THEN** 必须释放所有子模块

## Out of Scope

- pprof 数据分析方法
- HTTP 模块内部路由实现细节

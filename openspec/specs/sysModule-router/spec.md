# SysModule Router 玩家路由模块规范

## 目的

本规范描述 `engine/pkg/sysModule/router/` 当前代码已经实现的玩家路由缓存模块语义。

## Requirements

### Requirement: Router.OnInit 必须创建一个永久不过期的路由缓存

#### Scenario: 初始化时创建 go-cache 实例

- **GIVEN** 一个新的 `sysModule/router.Router`
- **WHEN** 调用 `OnInit()`
- **THEN** 必须创建一个默认不过期、清理间隔不过期的 `go-cache` 实例

### Requirement: OnRelease 必须清空所有缓存路由

#### Scenario: 模块释放时 flush 路由缓存

- **GIVEN** Router 当前持有路由缓存
- **WHEN** 调用 `OnRelease()`
- **THEN** 必须调用 `Flush()` 清空全部缓存项

## Out of Scope

- 玩家路由写入规则
- 会话绑定和断线迁移逻辑

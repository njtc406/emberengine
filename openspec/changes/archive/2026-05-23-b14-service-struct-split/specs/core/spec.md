## MODIFIED Requirements

### Requirement: Service.Init 必须完成运行时依赖装配

`core.Service` 必须在初始化阶段装配 Mailbox、TimerScheduler、日志、事件处理器、路由与可选授权器等依赖。Init 方法必须将组件创建委托给独立的子初始化方法，自身仅做编排调度。

#### Scenario: Init 失败时必须回滚已分配资源

- **GIVEN** Service 初始化过程中的任一子初始化步骤失败
- **WHEN** `Service.Init` 返回错误
- **THEN** Service 必须回滚已经创建的运行时资源
- **AND** 不得保留半初始化状态

#### Scenario: Init 编排方法简洁

- **WHEN** 查看 `Service.Init` 方法
- **THEN** 该方法必须不超过 50 行
- **AND** 组件创建逻辑必须位于 `service_init.go` 中的子方法内

## REMOVED Requirements

### Requirement: msgHooks 消息钩子支持

**Reason**: `msgHooks` 功能已被 Mailbox 中间件（`AddMailboxMiddlewares`）完全替代，代码中已标记为"暂时废弃"
**Migration**: 使用 `Service.AddMailboxMiddlewares()` 注册 `inf.IMailboxMiddleware` 实现相同功能

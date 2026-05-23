## 1. 废弃代码清理

- [x] 1.1 删除 `hook.go` 中的 `MsgHookFun` 类型定义和 `AddMsgHook` 方法
- [x] 1.2 删除 `service.go` 中 Service 结构体的 `msgHooks` 字段
- [x] 1.3 将 `AddMailboxMiddlewares` 方法从 `hook.go` 移至 `service.go`（或保留 hook.go 仅含此方法）
- [x] 1.4 运行 `go build ./...` 和 `go vet ./...` 验证编译通过

## 2. 运行时依赖包化

- [x] 2.1 在 `service.go` 中定义 `runtimeDeps` 内部 sub-struct（nodeCtx、cluster、endpointManager、profilerRegistry、router）
- [x] 2.2 将 Service 结构体的 5 个散落字段替换为 `deps runtimeDeps`
- [x] 2.3 更新 `SetRuntimeDeps` 方法写入 `s.deps`
- [x] 2.4 更新 `SetNodeContext`/`GetNodeContext` 读写 `s.deps.nodeCtx`
- [x] 2.5 更新 `GetEndpointManager`/`GetRouter` 读 `s.deps.xxx`，保持 fallback 逻辑不变
- [x] 2.6 更新 Init 和其他内部方法中所有 `s.cluster`/`s.endpointManager`/`s.router`/`s.nodeCtx`/`s.profilerRegistry` 引用
- [x] 2.7 运行 `go build ./...` 和 `go vet ./...` 验证编译通过

## 3. Init 方法分解

- [x] 3.1 创建 `engine/pkg/core/service_init.go` 文件
- [x] 3.2 提取 `initLogger(conf *config.ServiceInitConf) error` 方法（日志初始化逻辑）
- [x] 3.3 提取 `initTimers(conf *config.ServiceInitConf) error` 方法（TimerScheduler 创建）
- [x] 3.4 提取 `initMailbox(conf *config.ServiceInitConf) error` 方法（Mailbox + 中间件创建）
- [x] 3.5 提取 `initEvents() error` 方法（EventProcessor + EventHandler 创建）
- [x] 3.6 提取 `initConcurrent()` 方法（TaskScheduler 创建）
- [x] 3.7 提取 `initPID(conf *config.ServiceInitConf) error` 方法（EndpointManager 创建 PID + 日志字段追加）
- [x] 3.8 提取 `initRPC(conf *config.ServiceInitConf) error` 方法（MethodMgr + RpcHandler + Authorizer 注入）
- [x] 3.9 简化 `service.go` 中的 `Init()` 为编排方法（≤50 行），依次调用子方法
- [x] 3.10 运行 `go build ./...` 和 `go vet ./...` 验证编译通过

## 4. Profiler 桥接提取

- [x] 4.1 创建 `engine/pkg/core/service_profiler.go` 文件
- [x] 4.2 定义 `profilerBridge` 内部 struct，封装 Open/Close 逻辑
- [x] 4.3 将 `OpenProfiler` 和 `closeProfiler` 委托给 `profilerBridge`
- [x] 4.4 删除 `profilerRegistryAdapter` 类型（由 profilerBridge 内部处理）
- [x] 4.5 运行 `go build ./...` 和 `go vet ./...` 验证编译通过

## 5. 验证

- [x] 5.1 运行 `go test ./engine/pkg/core/...` 确保所有现有测试通过
- [x] 5.2 运行 `go test ./engine/pkg/services/...` 确保 ServiceManager 集成不受影响
- [x] 5.3 运行 `go test ./...` 全量测试（排除 CGO 依赖包）
- [x] 5.4 确认 `service.go` 行数 ≤ 400 行，`service_init.go` 包含所有 init* 方法

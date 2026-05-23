## Why

`core.Service` 结构体聚合了 25+ 个直属字段（加上 Module 嵌入共 40+ 字段），`Init()` 方法长达 180+ 行，集中完成 logger、mailbox、timer、event、RPC、PID 等全部组件的创建与装配。这导致可读性差、单元测试困难、新增依赖时修改面积大。B14 审计明确指出 "Service 聚合了过多职责"，需要进行内部重构。

## What Changes

- **Init 分解**：将 `Service.Init()` 拆分为 6 个内聚的私有子方法（`initLogger`、`initMailbox`、`initTimers`、`initEvents`、`initRPC`、`initPID`），Init 仅做编排
- **运行时依赖包化**：将 `cluster`、`endpointManager`、`profilerRegistry`、`router`、`nodeCtx` 五个散落字段收拢为 `runtimeDeps` 内部 sub-struct
- **废弃代码清理**：删除已废弃的 `msgHooks` 字段和 `AddMsgHook` 方法 **BREAKING**（无已知使用者）
- **Profiler 桥接提取**：将 `OpenProfiler` / `closeProfiler` 的 registry 交互逻辑提取为 `profilerBridge` 内部 helper

## Capabilities

### New Capabilities

- `service-init-decomposition`: Service.Init 方法拆分为可独立测试的子初始化步骤，编排逻辑与组件创建分离
- `service-runtime-deps`: 运行时依赖（cluster/endpoints/router/nodeCtx/profilerRegistry）收拢为 runtimeDeps 内部结构体

### Modified Capabilities

- `core`: Init 内部实现重构，Requirements 不变（装配行为、回滚语义、StopPolicy 优先级保持一致）

## Impact

- **受影响代码**：`engine/pkg/core/service.go`（主重构）、`engine/pkg/core/hook.go`（删除 msgHooks）
- **新增文件**：`engine/pkg/core/service_init.go`、`engine/pkg/core/service_profiler.go`
- **API 变更**：`MsgHookFun` 类型和 `AddMsgHook` 方法删除（Breaking，无外部使用者）
- **兼容性**：`IService` 接口不变、`core.Service` 嵌入 API 不变、`ServiceManager` Aware 注入方式不变
- **依赖**：无新增外部依赖

# errorlib 到 errorx 迁移方案

> 日期：2026-06-10  
> 状态：已完成  
> 原则：框架当前为全新项目，不保留向后兼容；`errorlib` 若可被 `errorx` 完全替代，则直接删除。

> 完成记录：已将 `msgbus` 聚合错误迁移到 `errorx.CombineErrors`，删除 `engine/pkg/utils/errorlib`，增强聚合错误测试，并通过相关测试验证。

## 1. 背景

当前框架已经具备新的结构化错误处理包 `engine/pkg/utils/errorx`，能力包括：

- 错误码：`errorx.New`、`errorx.WrapWithCode`、`errorx.HasCode`、`errorx.CodeFrom`
- 错误链：兼容 `errors.Is` / `errors.As` / `errors.Unwrap`
- 聚合错误：`errorx.CombineErrors`，底层使用 `errors.Join`
- 结构化上下文：`WithField` / `WithFields`
- RPC wire 序列化：`MarshalToBytes` / `UnmarshalFromBytes`

旧包 `engine/pkg/utils/errorlib` 只保留 legacy 错误码与字符串拼接式聚合错误。由于当前框架没有兼容负担，迁移目标是完全移除 `errorlib`，避免后续继续误用旧错误体系。

## 2. 目标

- 业务代码不再 import `engine/pkg/utils/errorlib`。
- `errorlib.CombineErr` 全部替换为 `errorx.CombineErrors`。
- 删除 `engine/pkg/utils/errorlib` 包及其测试。
- 聚合错误保留子错误身份，支持 `errors.Is` / `errors.As`。
- `docs/NEXT_GOALS.md` 中将 B-2-4 标注为完成。

## 3. 非目标

本轮不处理 `engine/pkg/def/error.go` 中全部 `errors.New` sentinel 的错误码化迁移。

原因：

- `def` sentinel 不属于 `errorlib`。
- 将所有 sentinel 改成 `errorx.New` 会改变错误字符串和部分测试预期。
- 应作为后续独立任务，按模块分批迁移，优先 RPC 跨节点错误。

## 4. 实施前 legacy 使用点快照

实施前经核查，业务侧主要剩余使用点集中在：

- `engine/pkg/rpc/message/msgbus/bus.go`
  - import `github.com/njtc406/emberengine/engine/pkg/utils/errorlib`
  - 9 处调用，分布如下：

  | 方法 | 行 | 调用模式 |
  |---|---|---|
  | `MultiBus.Call` | 719 | `errorlib.CombineErr(errs...)` |
  | `MultiBus.CallWithOpt` (CallModeAll) | 747 | `errorlib.CombineErr(errs...)` |
  | `MultiBus.CallWithOpt` (CallModeAny) | 760 | `errorlib.CombineErr(errs...)` |
  | `MultiBus.AsyncCall` | 786 | `errorlib.CombineErr(append(errs, monitorErr)...)` |
  | `MultiBus.AsyncCall` | 788 | `errorlib.CombineErr(errs...)` |
  | `MultiBus.AsyncCallWithOpt` | 828 | `errorlib.CombineErr(append(errs, monitorErr)...)` |
  | `MultiBus.AsyncCallWithOpt` | 830 | `errorlib.CombineErr(errs...)` |
  | `MultiBus.Send` | 853 | `errorlib.CombineErr(errs...)` |
  | `MultiBus.SendWithOpt` | 875 | `errorlib.CombineErr(errs...)` |

  替换方式：将 `errorlib.CombineErr` 改为 `errorx.CombineErrors`，删掉 `errorlib` import，新增 `errorx` import。

`errorlib.NewErrCode` 实施前未发现业务调用，仅旧包自身测试覆盖；旧包已在本轮迁移中删除。

## 5. 架构变更

### 5.1 `msgbus` 聚合错误改造

文件：`engine/pkg/rpc/message/msgbus/bus.go`

变更：

- 删除 `errorlib` import。
- 新增 `errorx` import。
- 替换聚合错误调用：

| 旧实现 | 新实现 |
|---|---|
| `errorlib.CombineErr(errs...)` | `errorx.CombineErrors(errs...)` |
| `errorlib.CombineErr(append(errs, monitorErr)...)` | `errorx.CombineErrors(append(errs, monitorErr)...)` |

收益：

- 旧实现通过字符串拼接丢失错误类型。
- 新实现基于 `errors.Join`，可以通过 `errors.Is` / `errors.As` 识别任意子错误。

### 5.2 删除 `errorlib`

`errorlib` 目录下共有 3 个文件：

- `engine/pkg/utils/errorlib/errors.go`
- `engine/pkg/utils/errorlib/errors_test.go`
- `engine/pkg/utils/errorlib/caller.go`

以上全部删除，目录也一并移除。

不保留 Deprecated shim，不做兼容层。

#### 影响评估

- `CError` 接口：仅 `errorlib` 自用，无业务代码引用（已 grep 确认无 `errorlib.CError` 或 `.(CError)` 类型断言）。
- `NewErrCode`：仅 `errorlib/errors_test.go` 调用，无业务引用。
- `caller.go`：内部辅助类型，仅 `errors.go` 使用。
- 结论：`errorlib` 全量删除对业务代码无影响。

### 5.3 文档更新

文件：`docs/NEXT_GOALS.md`

建议更新：

- 将 B-2-4 从“收尾中 / legacy 调用迁移”改为“已完成”。
- 将相关描述从“`errorlib` 已 Deprecated”改为“`errorlib` 已移除，统一使用 `errorx`”。

## 6. 实施步骤

### 阶段 1：替换业务调用

1. 修改 `engine/pkg/rpc/message/msgbus/bus.go`
   - 删除 `errorlib` import。
   - 增加 `errorx` import。
   - 全量替换 `errorlib.CombineErr` 为 `errorx.CombineErrors`。

2. 搜索确认无业务引用：
   - `errorlib`
   - `NewErrCode`
   - `CombineErr`
   - `CError`

### 阶段 2：增强现有测试

`bus_test.go` 已有以下聚合错误测试：

- `TestMultiBusCallWithOptAllAggregatesErrors`：CallModeAll 聚合错误
- `TestMultiBusSendAggregatesErrors`：Send 聚合错误

本轮需增强上述测试：

建议覆盖：

- 多节点调用全部失败时返回非 nil error。 ✅ 已有
- `errors.Is(err, sentinelErr1)` 为 true。 🆕 新增
- `errors.Is(err, sentinelErr2)` 为 true。 🆕 新增
- 全部成功时返回 nil。 ✅ 已有

**注意**：`TestMultiBusSendAggregatesErrors` 保留了 `strings.Contains(err.Error(), "e1")` 的兼容性检查，同时新增 `errors.Is` 检查，确保迁移后既保留基本错误文本可读性，也验证 joined error 可命中子错误。

### 阶段 3：删除旧包

1. 删除 `engine/pkg/utils/errorlib/errors.go`。
2. 删除 `engine/pkg/utils/errorlib/errors_test.go`。
3. 删除 `engine/pkg/utils/errorlib/caller.go`。
4. 删除空目录 `engine/pkg/utils/errorlib/`。
5. 确认仓库内无 `errorlib` 引用。

### 阶段 4：更新规划文档

1. 更新 `docs/NEXT_GOALS.md`。
2. 标注 B-2-4 已完成。
3. 将后续任务收敛为 `def/error.go` 中 RPC sentinel 的错误码化迁移。

## 7. 测试策略

### 必跑测试

```powershell
go test ./engine/pkg/rpc/message/msgbus -count=1
go test ./engine/pkg/utils/errorx -count=1
```

### 建议测试

```powershell
go test ./engine/pkg/rpc/... ./engine/pkg/utils/errorx -count=1
```

### 最终验证

```powershell
go test ./engine/pkg/... -count=1
```

## 8. 风险与缓解

### 风险 1：聚合错误字符串格式变化

旧 `errorlib.CombineErr` 使用字符串拼接；新 `errorx.CombineErrors` 使用 `errors.Join`。

缓解：

- 当前无兼容负担，可接受格式变化。
- 测试与业务逻辑应使用 `errors.Is` / `errors.As`，不要依赖错误字符串。

### 风险 2：删除 `errorlib` 后仍有遗漏 import

缓解：

- 删除前后分别搜索 `errorlib|NewErrCode|CombineErr|CError`。
- 使用 `go test ./engine/pkg/...` 兜底编译检查。

### 风险 3：`errors.Join` 与旧拼接行为不同

差异：

- `errors.Join(nil, err)` 会过滤 nil。
- 单个非 nil error 会直接返回原错误。
- 多个错误会形成 joined error。

缓解：

- 这是期望行为。
- 新增测试明确聚合语义。

## 9. 后续任务：def sentinel 错误码化

本轮完成后，下一步可单独迁移 `engine/pkg/def/error.go` 中的 RPC 关键 sentinel。

建议首批：

- `ErrRPCCallTimeout`
- `ErrRPCCallFailed`
- `ErrServiceNotFound`
- `ErrMethodNotFound`
- `ErrRPCHadClosed`
- `ErrMsgSerializeFailed`
- `ErrRpcMsgMetaOrDataIsNil`

建议先定义错误码常量：

| 错误码范围 | 模块 |
|---|---|
| `1200-1299` | RPC 调用链 |
| `1600-1699` | Node 生命周期 |
| `1700-1799` | Router |
| `9000-9099` | 通用错误 |

## 10. 成功标准

- [x] `engine/pkg/rpc/message/msgbus/bus.go` 不再 import `errorlib`。
- [x] 仓库内无业务代码引用 `errorlib`。
- [x] `engine/pkg/utils/errorlib` 已删除。
- [x] `MultiBus` 聚合错误支持 `errors.Is` 命中子错误。
- [x] `go test ./engine/pkg/rpc/message/msgbus -count=1` 通过。
- [x] `go test ./engine/pkg/utils/errorx -count=1` 通过。
- [x] `docs/NEXT_GOALS.md` 标注 B-2-4 已完成。
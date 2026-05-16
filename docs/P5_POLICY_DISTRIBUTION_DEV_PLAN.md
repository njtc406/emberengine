# 实施方案：P5 策略存储与分发

## 概述

在已完成的内存 RBAC 引擎基础上，补齐策略的可配置、可分发、可热更新能力。目标是让 `Authorizer` 不再只能由代码手动 `AddRole/BindRole`，而是能从本地配置或 etcd 策略目录加载完整快照，并通过 watch 增量刷新到运行时授权决策中。

本阶段只做策略存储与分发闭环，不实现审计日志和管理控制台；默认保持 RBAC 关闭，避免影响现有开发和示例启动路径。

## 需求

- 支持声明式 RBAC 策略：enabled、roles、bindings、version/revision。
- 支持本地策略文件加载，作为单机和测试环境的最小可用路径。
- 支持 etcd 策略存储与 watch 更新，作为集群生产配置路径。
- Authorizer 策略应用必须原子化：新策略校验成功后一次性替换，校验失败保留旧快照。
- RBAC 启用后，初始策略未加载成功时默认 fail-closed；可通过显式配置开启 fail-open。
- 所有更新路径要有单元测试覆盖，禁止依赖真实 etcd 才能通过测试。

## 架构变更

- `engine/pkg/authz/policy.go`：新增 `PolicySnapshot`、`PolicyRole`、`PolicyBinding`、校验与标准化逻辑。
- `engine/pkg/authz/store.go`：新增 `PolicyStore` 接口、本地文件 store、watch 事件模型。
- `engine/pkg/authz/etcd_store.go`：新增 etcd policy store，复用 `config.ETCDConf` 构造 client，watch 指定 prefix。
- `engine/pkg/authz/watcher.go`：新增 `PolicyWatcher`，负责初始加载、watch 循环、重试、停止。
- `engine/pkg/authz/authz.go`：为 `Authorizer` 增加 `ApplySnapshot`、`Snapshot`、`Revision` 等原子快照方法。
- `engine/pkg/config/define.go`：新增 `AuthzConf`，挂到 `ClusterConf` 或顶层 Config 的可选字段。
- `engine/pkg/node/node.go`：在 Node Init 阶段根据 `AuthzConf` 初始化策略加载器，并把 Stop 清理纳入现有逆序清理链。
- `template/config/node.yaml`、`example/configs/**/node.yaml`：补充可选 authz 配置示例，默认 disabled。
- `docs/P5_SECURITY_DEV_PLAN.md`、`docs/ROADMAP.md`、`docs/NEXT_GOALS.md`：同步 P5-10~P5-15 状态。

## 策略格式建议

```yaml
AuthzConf:
  Enable: false
  Source: local        # local | etcd
  FailOpen: false
  InitialLoadTimeout: 3s
  LocalPolicyPath: ./configs/authz_policy.yaml
  EtcdPolicyPrefix: /ember/authz/policies
```

本地策略文件：

```yaml
version: 1
revision: 2026051401
roles:
  game:
    permissions:
      - DataService.Get*
      - CacheService.*
  admin:
    permissions:
      - "*"
bindings:
  GameService:
    serviceTypes:
      - game
    roles:
      - game
  AdminService:
    serviceTypes:
      - admin
    roles:
      - admin
```

etcd 路径建议：

```text
/ember/authz/policies/snapshot      # 完整策略快照 JSON/YAML
/ember/authz/policies/revision      # 可选：当前 revision，便于人工检查
```

第一版优先完整快照更新，不做逐条 role/binding patch，降低一致性和回滚复杂度。

## 实施步骤

### 阶段 1：策略模型与 Authorizer 快照

1. **定义策略快照结构**（文件：`engine/pkg/authz/policy.go`）
   - 操作：新增 `PolicySnapshot`、`PolicyRole`、`PolicyBinding`、`Validate()`、`Normalize()`。
   - 原因：把运行时 RBAC 表从手写 API 迁移到可序列化策略模型。
   - 依赖项：现有 `Role`、`Authorizer`。
   - 风险：低。

2. **增加原子应用接口**（文件：`engine/pkg/authz/authz.go`）
   - 操作：新增 `ApplySnapshot(snapshot PolicySnapshot) error`，内部先校验后在单个写锁内替换 roles/bindings/enabled/revision。
   - 原因：watch 更新不能产生半应用状态。
   - 依赖项：步骤 1。
   - 风险：中；必须保留现有 `AddRole/BindRole` 测试兼容。

3. **增加快照读取能力**（文件：`engine/pkg/authz/authz.go`）
   - 操作：新增 `Snapshot() PolicySnapshot` 或只读 `Revision()`，测试和诊断可用。
   - 原因：方便验证策略是否按预期生效。
   - 依赖项：步骤 2。
   - 风险：低。

### 阶段 2：本地策略加载 MVP

4. **定义 PolicyStore 接口**（文件：`engine/pkg/authz/store.go`）
   - 操作：新增 `Load(ctx) (PolicySnapshot, error)`、`Watch(ctx) (<-chan PolicyEvent, error)`、`Close() error`。
   - 原因：让本地文件和 etcd 后端共享同一 watcher。
   - 依赖项：步骤 1。
   - 风险：低。

5. **实现本地文件 Store**（文件：`engine/pkg/authz/store.go` 或 `local_store.go`）
   - 操作：用现有配置解析风格读取 YAML/JSON 策略；第一版可不做文件系统 watch，只在启动加载。
   - 原因：提供无需 etcd 的最小交付路径和稳定测试入口。
   - 依赖项：步骤 4。
   - 风险：低。

6. **添加策略模型测试**（文件：`engine/pkg/authz/policy_test.go`）
   - 操作：覆盖空角色、未知角色绑定、非法权限、重复 role、enabled=false、通配符策略。
   - 原因：策略校验是安全边界。
   - 依赖项：步骤 1-5。
   - 风险：低。

### 阶段 3：PolicyWatcher 与 Node 生命周期接入

7. **实现 PolicyWatcher**（文件：`engine/pkg/authz/watcher.go`）
   - 操作：启动时 `Load` 一次；watch 事件到达后重新加载或应用新快照；支持 `Start/Stop` 幂等。
   - 原因：把分发逻辑从 Node 和 Authorizer 中隔离出来。
   - 依赖项：步骤 4-5。
   - 风险：中；goroutine 退出和取消必须测试。

8. **新增 AuthzConf 配置**（文件：`engine/pkg/config/define.go`）
   - 操作：新增 `AuthzConf`：`Enable`、`Source`、`FailOpen`、`LocalPolicyPath`、`EtcdPolicyPrefix`、`InitialLoadTimeout`、`WatchRetryInterval`。
   - 原因：让策略源和失败语义可显式配置。
   - 依赖项：步骤 7。
   - 风险：中；配置默认值必须保持向后兼容。

9. **接入 Node 初始化与停止链**（文件：`engine/pkg/node/node.go`）
   - 操作：创建 `Authorizer` 后根据 `AuthzConf` 构建 store/watcher；watcher 成功启动后加入 `stopCleanups`。
   - 原因：让策略分发生命周期跟随 Node。
   - 依赖项：步骤 7-8。
   - 风险：中；RBAC disabled 时必须零行为变化。

10. **补充 Node/Authz 配置测试**（文件：`engine/pkg/config/config_test.go`、`engine/pkg/node/*_test.go`）
    - 操作：覆盖默认 disabled、local 策略加载成功、local 策略缺失 fail-closed/fail-open。
    - 原因：防止生产配置误用导致授权全放行或全拒绝。
    - 依赖项：步骤 8-9。
    - 风险：中。

### 阶段 4：etcd 策略 Store

11. **实现 EtcdPolicyStore**（文件：`engine/pkg/authz/etcd_store.go`）
    - 操作：基于 `config.ETCDConf` 创建 client；从 `EtcdPolicyPrefix/snapshot` 读取完整快照；watch prefix 后触发 reload。
    - 原因：提供集群生产策略分发路径。
    - 依赖项：步骤 4、8。
    - 风险：中；连接失败和 watch 中断必须保持旧策略。

12. **处理 watch 重连与错误退避**（文件：`engine/pkg/authz/watcher.go`）
    - 操作：watch channel 错误后按 `WatchRetryInterval` 重建；取消时立即退出。
    - 原因：etcd 连接波动不能泄漏 goroutine，也不能清空现有策略。
    - 依赖项：步骤 11。
    - 风险：中。

13. **添加 etcd store 单元测试**（文件：`engine/pkg/authz/etcd_store_test.go`）
    - 操作：不要依赖真实 etcd；抽象最小 `kvClient` fake，覆盖 load、watch put、watch delete、错误保留旧快照。
    - 原因：CI 不应依赖外部服务。
    - 依赖项：步骤 11-12。
    - 风险：低。

### 阶段 5：模板、文档与门禁

14. **补齐配置模板**（文件：`template/config/node.yaml`、`example/configs/**/node.yaml`）
    - 操作：增加注释化 authz 配置，默认 `Enable: false`。
    - 原因：让用户知道功能存在，但不破坏示例。
    - 依赖项：步骤 8。
    - 风险：低。

15. **补充配置参考文档**（文件：`docs/CONFIG_REFERENCE.md`）
    - 操作：新增 AuthzConf 字段说明、默认值、生产建议。
    - 原因：策略分发属于运维配置，必须可查。
    - 依赖项：步骤 8、14。
    - 风险：低。

16. **回填 P5 文档状态**（文件：`docs/P5_SECURITY_DEV_PLAN.md`、`docs/ROADMAP.md`、`docs/NEXT_GOALS.md`）
    - 操作：P5-10~P5-15 完成后标记，归档策略分发里程碑。
    - 原因：保持阶段进度可追踪。
    - 依赖项：步骤 1-15。
    - 风险：低。

## 测试策略

- 单元测试：`engine/pkg/authz/policy_test.go` 覆盖策略校验、标准化和权限应用。
- 单元测试：`engine/pkg/authz/watcher_test.go` 覆盖初始加载、watch 更新、无效策略保留旧快照、Stop 幂等。
- 单元测试：`engine/pkg/authz/etcd_store_test.go` 使用 fake kv client，覆盖 etcd load/watch 错误路径。
- 配置测试：`engine/pkg/config/config_test.go` 覆盖 AuthzConf 默认值和反序列化。
- 集成测试：`engine/pkg/core/rpc/handler_test.go` 或新增 authz handler 测试，验证策略热更新后 RPC 授权结果变化。
- 全量门禁：`go test ./... -count=1`、`go vet ./...`、`go build ./...`。

## 风险与缓解措施

- **风险**：策略更新半成功导致某些角色丢失。
  - 缓解措施：快照先完整校验，成功后单写锁整体替换。
- **风险**：etcd 短暂不可用导致所有调用被拒绝。
  - 缓解措施：已加载快照继续生效；只有启用 RBAC 且初始加载失败时 fail-closed，必要时显式 `FailOpen=true`。
- **风险**：watch 乱序或 delete 事件造成旧策略覆盖新策略。
  - 缓解措施：使用完整快照 + revision；只接受 revision 单调递增的策略。
- **风险**：配置错误造成生产全放行。
  - 缓解措施：`Enable=true` 时无有效策略默认拒绝；`FailOpen` 必须显式配置且记录 warning。
- **风险**：测试依赖真实 etcd 不稳定。
  - 缓解措施：store 内部抽象最小 kv client，单元测试使用 fake。

## 成功标准

- [x] `Authorizer` 能从 `PolicySnapshot` 原子应用 roles/bindings。
- [x] 本地策略文件可加载，默认 disabled 行为保持不变。
- [x] etcd snapshot 路径可加载，watch 更新后授权结果可变化。
- [x] 无效策略不会覆盖旧策略。
- [x] RBAC enabled 且初始策略加载失败时默认 fail-closed。
- [x] watcher Stop 幂等且无 goroutine 泄漏。
- [x] 新增 authz/policy/watcher/store 测试通过。
- [x] `go test ./... -count=1`、`go vet ./...`、`go build ./...` 全绿。
# EmberEngine TODO（服务容器视角）

> 目标：将 EmberEngine 打磨成一个以 Actor + RPC 为内核的通用 **服务运行时 / 服务容器**，
> 通过 Node → Service → Module → Component 模型承载游戏服、HTTP 服务以及其他实时服务。
>
> 设计约束：
> - **唯一的独立单位是 Service**；
> - Module / Component 仅在 Service 内挂载，不作为独立注册/访问主体；
> - 对外能力统一以 Service 为主体暴露（包括 Service 根模块及其子 Module 的 RPC 接口）；
> - Node 只统一“如何启动哪些 Service”，具体挂载哪些 Module 完全由业务决定（通过 Service 的 AddModule/ReleaseModule 等接口）。

---

## 1. 核心运行时模型（Node / Service / Module / Component）

> 这一部分主要是**澄清与打磨已有抽象**，不强行收紧业务扩展点。

- [ ] **澄清并固化运行时抽象（注释级）**
  - [ ] 在 `engine/pkg/core` 中梳理 `service.go`、`module.go`、`component/` 的职责边界（以注释为主）：
    - [ ] Service：唯一独立单位，负责消息队列、生命周期、配置、对外交互（RPC）。
    - [ ] Module：Service 内的业务功能模块，挂在 Service 根模块下组成树形结构，仅通过 Service 对外暴露能力。
    - [ ] Component：更细粒度的技术组件/库级能力（当前为空，保留扩展位）。
  - [ ] 在 Service/Module 相关接口或结构体上增加简短注释，说明：
    - [ ] Service 本身也是“根模块”；
    - [ ] 其他 Module 应挂载在根模块或父 Module 下，形成树结构；
    - [ ] Module/Component 不作为独立注册/发现/调用主体。

- [ ] **Service 执行模型配置化（单线程 / 多 Worker）**
  - [ ] 在 `engine/pkg/def/concurrent.go` / 配置结构中：
    - [ ] 统一 Service 的并发配置入口（如 `concurrency: 1|N`）。
    - [ ] 明确注释“1 = Actor 风格（严格串行）；N>1 = 多 Worker 并发模型（队列 + WorkerPool）”。
  - [ ] 在示例配置 `configs/node_concurrency*` 中：
    - [ ] 给出典型配置例子（逻辑服、匹配服、HTTP 服）。

- [ ] **Module 生命周期约定（以 Service 为中心）**
  - [ ] 检查 `engine/pkg/core/module.go` 与 `sysModule`：
    - [ ] 保证 Module 的 Init/Start/Stop 等生命周期接口与 Service 的整体生命周期兼容；
    - [ ] 明确 Module 仅能通过所属 Service 的 AddModule/ReleaseModule 等接口统一挂载/卸载（不额外提供全局加载入口）。
  - [ ] 在 `interfaces/IModule.go` 上补充注释，给出推荐实现思路（根模块 + 子模块树）。

- [ ] **Component 扩展位占坑**
  - [ ] 在 `engine/pkg/core/component` 中：
    - [ ] 添加文件注释，说明 Component 的预期职责（认证、存储、metrics、限流等技术性能力），但不强制使用；
    - [ ] 定义一个尽可能精简的 `IComponent` 最小接口（如 Init/Start/Stop/Name），方便未来按需填充。

---

## 2. 并发模型 & Mailbox & 异步执行

> 已有 mailbox/worker 基础并发模型，主要是补充说明和为未来的“并发组件 + 异步回投”模式占坑。

- [ ] **Mailbox/Worker 并发模型梳理（注释与示例）**
  - [ ] 在 `engine/pkg/actor/mailbox` 和 `engine/pkg/def/concurrent.go` 中：
    - [ ] 补充注释说明：
      - 单 Worker = Actor 风格（严格串行，适用于强顺序逻辑服/房间服）；
      - 多 Worker = 队列 + WorkerPool 并发模型，可配合 UID 粘连保证局部顺序；
      - 并发度由 Service 配置控制，而非硬编码。

- [ ] **异步执行组件（并发组件）设计占坑**
  - [ ] 在 `engine/pkg/def` 或合适位置设计一个简单的并发组件接口草案，例如 `IAsyncExecutor`：
    - [ ] `Submit(task)` / `SubmitWithContext(ctx, task)`；
    - [ ] 并发度/池大小配置；
    - [ ] 错误与超时的最小处理约定。
  - [ ] 在注释中说明推荐模式：
    - [ ] Worker 从 mailbox 取任务后，可将重 IO/重 CPU 操作提交给并发组件；
    - [ ] 并发组件完成后，通过“结果消息”回投当前 Service 的 mailbox，实现完整异步链路；
    - [ ] 顺序性语义由业务自行设计（例如为独立异步任务分配 TaskId/版本号）。

- [ ] **示例完善（并发 + 回投 mailbox）**
  - [ ] 在 `example/cache/comm/test_mailbox_service.go` 等位置：
    - [ ] 规划一个示例用例（可稍后实现）：
      - Service 收到请求 -> 提交到并发组件 -> 完成后作为“结果事件”再投递回 mailbox -> 日志输出。

---

## 3. Node / Cluster / Systemd / 启动流程

> Node 只负责“如何启动哪些 Service”，不干预业务 Service 内部挂载哪些 Module。

- [ ] **Node 启动/关闭流程梳理**
  - [ ] 在 `engine/pkg/node` 中：
    - [ ] 用注释梳理 Node 的生命周期（初始化 → 从配置加载 StartServices → 启动基础 Service → 注册到集群 → 优雅退出）。
  - [ ] 明确 Node 与 `template/config/node.yaml` 的对应关系：
    - [ ] 强调：仅统一“哪些 Service 在节点启动时自动拉起”，其他 Service 可由业务在运行时按需启动。

- [ ] **Cluster / Discovery / Routing 一致性**
  - [ ] 在 `engine/pkg/cluster` + `discovery` + `router` 中：
    - [ ] 列清 Node/Service 的注册信息结构，确保路由策略（按 ServiceName/ServiceType/标签）与之保持一致；
    - [ ] 标注 TODO：
      - [ ] 完善基于 ServiceType/标签的路由策略（例如分组路由、灰度路由等，作为未来增强）。

- [ ] **示例节点矩阵检查**
  - [ ] 检查 `example/node_*`：
    - [ ] 核实是否覆盖以下场景：
      - 单节点本地运行（`node_local`）。
      - 主从节点（`node_master` / `node_slave*`）。
      - 并发模式（`node_concurrency*`）。
    - [ ] 在 TODO 中记录尚未覆盖的典型场景（如：同时挂载逻辑 Service 与 HTTPModule 的混合 Node 示例）。

---

## 4. RPC / Gate / HTTP 模块（sysModule）

> RPC 和 sysModule 已经能用，这里主要是抽离/注释化和作为示例模块的定位。

- [ ] **RPC 抽象与解耦占坑**
  - [ ] 在 `engine/pkg/core/rpc` / `engine/pkg/rpc` / `interfaces/IRpc.go` 中：
    - [ ] 标记 TODO：“将 RPC 抽出为可独立项目（已在 README TODO 中），并在 Ember 中保留适配层”；
    - [ ] 不改变当前可用性，仅为未来拆分预留接口设计空间。

- [ ] **sysModule/httpmodule 作为系统/示例 Module 的定位**
  - [ ] 在 `engine/pkg/sysModule/httpmodule`（或对应路径）中：
    - [ ] 增加注释：
      - [ ] 说明这是一个“通过 Module 嵌入 gin 的 HTTP 服务模块示例/系统模块”；
      - [ ] 明确业务方可以自由定义自己的 Module，不必照此模板实现。

- [ ] **Gate / IGateHandler 接口通用性检查**
  - [ ] 在 `interfaces/IGateHandler.go` + 相关 gate 实现中：
    - [ ] 检查 IGateHandler 是否足够通用，以支持：
      - [ ] HTTP 请求；
      - [ ] WebSocket / TCP 连接（如有需要）；
    - [ ] 标记 TODO：
      - [ ] 如需为不同协议提供单独的 gate module（HTTPGateModule / WSGateModule），在接口上预留扩展点。

---

## 5. 日志 / 监控 / Profiling

> 日志已可用，这里侧重于固定字段封装的使用示例，以及监控/Profiling 的容器级入口。

- [ ] **LoggerX / 固定字段封装使用示例**
  - [ ] 在 `engine/pkg/log/logx.go` 中：
    - [ ] 保持当前“基于 WithFields 的固定字段封装，不覆写 ILogger 接口”的设计；
    - [ ] 标记 TODO：
      - [ ] 在某个 Service 示例中使用 LoggerX 创建带固定 `mod`/`service` 字段的 logger，体现推荐用法。

- [ ] **日志上下文 / Caller 行为说明**
  - [ ] 在 `engine/pkg/log/zap_core.go` + `engine/pkg/log/logger.go` 中：
    - [ ] 注释说明：
      - [ ] Caller 的计算依赖 zap 的 `AddCaller` / `AddCallerSkip`（以及本项目的 caller 相对路径编码逻辑）；
      - [ ] 避免在日志调用上增加额外包装层导致 caller 错位（如确需包装，务必正确调整 caller skip）。

- [ ] **监控/Profiler 接入点说明**
  - [ ] 在 `engine/pkg/monitor` / `profiler` 中：
    - [ ] 标记容器级的监控入口（节点级 metric、Service 粒度监控）；
    - [ ] 为后续监控/统计 Module（见第 9 节）预留挂载点。

---

## 6. 配置系统 & 模板

> 配置已可用，这一节主要是对齐代码结构与模板，并为并发/模块开关提供更清晰示例。

- [ ] **配置结构与模板对齐**
  - [ ] 在 `engine/pkg/config` + `template/config/*.yaml` 中：
    - [ ] 检查 Node/Service 的配置字段是否与代码结构一致（如并发配置、日志配置、RPC 配置）；
    - [ ] 标记 TODO：
      - [ ] 为 Service 并发模型、监控/统计 Module 开关等增加清晰的配置示例和注释说明。

- [ ] **代码中的默认值与模板中的默认配置同步**
  - [ ] 确认 `fixConf` 等默认值逻辑与模板中的默认配置不冲突，必要时备注差异原因。

---

## 7. 示例与测试

> 示例与测试更多是“可用性与回归保障”，按服务容器视角重组示例目录。

- [ ] **示例用例按“服务容器”视角整理**
  - [ ] 调整/规划 `example/` 下示例：
    - [ ] 将示例分为：
      - Node/Service 生命周期示例；
      - 并发 vs 单线程 Service 示例；
      - HTTPModule/sysModule 示例；
      - Actor 示例；
      - 集群/主从示例。

- [ ] **基础单元测试覆盖容器关键路径**
  - [ ] 在可能的情况下，为以下模块增加/整理测试用例：
    - [ ] event bus（已存在测试，检查是否需要补充多并发场景）；
    - [ ] mailbox/selector（顺序性/粘连/优先级）；
    - [ ] Node 启动/关闭的简单 smoke test（构造最小 Node + 基础 Service 启动/停止）。

---

## 8. 文档与设计记录（后期再完善）

> 当前阶段不强求完整文档，仅记录需要在“相对稳定版本”后补充的文档方向。

- [ ] **执行模型设计文档（Actor vs 多 Worker vs 异步组件）**
  - [ ] 描述 mailbox/worker 模式、顺序性保证、并发组件的角色和模式，结合示例说明典型使用场景。

- [ ] **模块/组件扩展指南（以 Service 为中心）**
  - [ ] 后续提供“如何在 Service 中挂载自定义 Module / Component”的简单指南和示例，强调：
    - [ ] Module 不作为独立单位，仅通过 Service 使用；
    - [ ] 推荐的模块树组织方式（根模块 + 子模块）。

- [ ] **节点/集群部署示例**
  - [ ] 基于 `template/docker/*` 和 `configs/node_*` 完整梳理：
    - [ ] 单机多节点；
    - [ ] 主从集群；
    - [ ] 带 HTTPModule 的节点部署。

---

## 9. 未来可扩展能力（功能级，非必须）

> 以下为功能级增强方向，当前不影响框架可用性，可在框架稳定后按需演进。

- [ ] **Service 运行时状态统计与查询**
  - [ ] 在 Service 中增加简单的运行时状态统计结构（如 Init/Starting/Running/Draining/Stopped/Failed）；
  - [ ] 将状态变更事件纳入统计/监控体系（例如打点或写入集群状态中，供其他节点查询）。

- [ ] **AB 滚动更新基础支撑（跨 Node）**
  - [ ] 规划 Service 级的 AB 切换机制（不要求在同一 Node 内启 A/B）：
    - [ ] 在集群/路由层预留“版本/Group/Tag”等字段，用于标识 A/B 版本；
    - [ ] 提供最小的接口/配置支持，让上层业务可以实现滚动更新/灰度发布（具体策略由业务决定）。

- [ ] **监控 / 健康检查 / 统计 Module 实现**
  - [ ] 基于第 5 节的挂载点和第 6 节的配置开关，设计并实现：
    - [ ] HealthCheckModule：Service 粒度的健康检查（如队列长度、worker 状态、RPC ping 等），可通过 HTTP/RPC 暴露健康接口；
    - [ ] MetricsModule：采集 Service 粒度的 QPS、RT、错误率等指标，对接 `monitor` 或外部监控系统；
    - [ ] StatsModule（可选）：更细粒度统计/trace 能力（可作为后续增强）。

- [ ] **Service 内模块树的最小查询 API**
  - [ ] 在 Service 或 Module 接口中预留基础查询能力（如按名称/path 查找子模块），以便业务在 Service 内部管理和组合模块树。

---

本清单以“服务容器”的视角划分子系统和 TODO，
分为“需要改进/完善的现有能力”（1–8）和“未来可扩展能力”（第 9 节）。
可以按模块/文件逐步推进，并在实现演进中补充或调整。

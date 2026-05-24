# Service 生命周期与集群可见性设计

> 日期：2026-05-24
>
> 目标：重新梳理 Service 启动状态、集群注册时机与服务可见性策略，解决“服务已被集群选中但 `OnStarted()` 尚未完成”和“RPC 方法存在即全量注册到集群导致私有/半私有服务规模膨胀”的问题。

---

## 1. 背景

当前 Service 启动链路位于 `engine/pkg/core/service.go`：

```text
Start()
  -> mailbox.Start()
  -> startListenCallback()
  -> OnStart()
  -> EndpointManager.AddService()
  -> setStatus(SvcStatusRunning)
  -> OnStarted()
```

`EndpointManager.AddService()` 位于 `engine/pkg/cluster/endpoints/endpoints.go`，当前语义同时包含两个动作：

1. 将本地服务加入 `Repository`，使本节点可以通过 PID 找到本地 mailbox。
2. 如果服务不是私有服务并且处于集群模式，则触发 `SysEventServiceReg`，由 etcd discovery 发布到集群。

这导致 `OnStarted()` 执行前，服务已经可能被其他节点发现并选中。对于需要在 `OnStarted()` 中完成数据预热、订阅、依赖检查、集群协作初始化的服务而言，这个时机过早。

同时，当前 `IsPrivate()` 的判断来自 `MethodMgr.IsPrivate()`：只要存在 `Rpc`/`RPC` 前缀方法，服务就不是私有服务。这个规则把“方法是否可被 RPC 调用”和“服务是否应该注册到集群发现”耦合在一起，不够灵活。例如玩家服务虽然有 RPC 方法，但不应该因为每个玩家都有可调用接口就全部注册到 etcd 并进入全局路由候选集。

引入显式可见性后，`api`/`rpc` 前缀不应再决定服务是否私有。方法前缀只描述接口的调用入口和暴露风格；服务是否发布到集群、是否允许远端定向调用，应统一由 `ServiceVisibility` 决定。

---

## 2. 设计目标

- Service 可以先被集群感知，但未 Ready 前不能被普通路由选择。
- `OnStarted()` 阶段允许服务进行依赖其他集群服务的初始化，并能接收必要的定向回复。
- 集群服务发现规模与“公共可发现服务数量”相关，而不是与“所有拥有 RPC 方法的实体服务数量”相关。
- 区分“可发现路由”和“持有 PID 的定向访问”。
- 配置未声明可见性时默认按私有服务处理，避免服务因新增 RPC/API 方法意外进入集群发现。
- 兼容旧行为必须显式选择迁移模式，不能作为新配置默认值。

---

## 3. 问题一：服务生命周期状态与注册时机

### 3.1 当前问题

当前 `AddService()` 发生在 `OnStarted()` 前，且 etcd 注册和本地缓存注册绑定在一起。

风险包括：

- 其他节点 watch 到服务上线后立刻加入候选集，可能把业务请求发给尚未完成 `OnStarted()` 的服务。
- `OnStarted()` 失败时，服务已经短暂对集群可路由，形成启动失败窗口。
- 运维侧无法区分“服务进程已启动”和“服务业务已就绪”。

### 3.2 新状态模型

在 `engine/pkg/def/consts.go` 中新增 `SvcStatusReady`，建议状态顺序为：

```go
const (
    SvcStatusUnknown  int32 = iota // 未运行
    SvcStatusInit                  // 初始化
    SvcStatusStarting              // 启动中
    SvcStatusRunning               // 已运行，已进入集群缓存，但不可被普通路由选中
    SvcStatusReady                 // 已就绪，可被普通路由选中
    SvcStatusClosing               // 关闭中
    SvcStatusClosed                // 关闭
    SvcStatusRetire                // 退休
)
```

语义区分：

| 状态 | 本地 Repository | etcd/集群缓存 | 普通路由可选中 | 定向 PID 调用 |
| ------ | ----------------- | --------------- | ---------------- | --------------- |
| Starting | 否 | 否 | 否 | 否 |
| Running | 是 | 是 | 否 | 是 |
| Ready | 是 | 是 | 是 | 是 |
| Closing/Closed | 移除中/已移除 | 删除中/已删除 | 否 | 否 |

### 3.3 两阶段集群更新

启动时对集群进行两次状态发布：

```text
Start()
  -> mailbox.Start()
  -> startListenCallback()
  -> OnStart()
  -> resolve master/secondary
  -> setStatus(SvcStatusRunning)
  -> EndpointManager.AddService(svc)
       - 本地 Repository 添加 dispatcher
       - etcd 发布 Running 状态
       - 其他节点加入缓存，但路由选择跳过
  -> OnStarted()
       - 可以调用其他服务
       - 可以接收基于 PID 的定向回复
       - 但不会被普通服务发现流量选中
  -> setStatus(SvcStatusReady)
  -> EndpointManager.ServiceReady(svc)
       - etcd 更新 Ready 状态
       - 其他节点更新缓存状态
       - 路由选择开始允许选中
```

这比延迟到 `OnStarted()` 后才注册更稳妥，因为 `OnStarted()` 中如果需要发起 RPC，远端服务已经能通过 Running 状态的缓存知道本服务存在，回复链路更自然。

### 3.4 Repository 选择规则

Repository 需要区分两类访问：

1. **普通路由选择**：按 service name/type/partition 等条件选择候选服务。
   - 只能选择 `SvcStatusReady` 的服务。
   - `SvcStatusRunning` 只保留缓存，不参与负载均衡和随机选择。

2. **定向 PID 调用**：调用方已经持有完整 PID。
   - 可以访问 `SvcStatusRunning` 和 `SvcStatusReady` 的服务。
   - 用于启动期 reply、回调、持有 UID/PID 的业务定向访问。

### 3.5 失败与回滚

`OnStarted()` 失败时必须执行回滚：

```text
OnStarted() returns error
  -> EndpointManager.RemoveService(svc)
  -> mailbox/timer/concurrent 按现有 Stop/release 语义清理
  -> set status to Closing/Closed or return startup error to ServiceManager
```

需要避免 Running 状态服务长期残留。若节点崩溃，etcd lease 仍然负责最终删除；但正常错误返回路径必须主动 Remove。

### 3.6 影响点

- `engine/pkg/def/consts.go`
  - 新增 `SvcStatusReady`。
  - 检查所有 `> SvcStatusRunning` 的关闭判断，尤其 `Service.IsClosed()`，需要改为基于 `SvcStatusReady` 或 `SvcStatusClosing`。

- `engine/pkg/core/service.go`
  - 调整 `Start()` 状态切换顺序。
  - `OnStarted()` 成功后再进入 Ready 并触发第二次集群更新。
  - `OnStarted()` 失败时主动回滚服务注册。

- `engine/pkg/cluster/endpoints/endpoints.go`
  - `AddService()` 保留 Running 注册语义。
  - 新增 `ServiceReady(svc IService)` 或 `UpdateServiceStatus(svc IService, status int32)`。
  - `RemoveService()` 继续负责下线通知。

- `engine/pkg/cluster/discovery/etcd/*`
  - etcd value 需要携带服务状态。
  - Running 和 Ready 都是 put/update 事件，不应被当成重复注册错误。

- `engine/pkg/cluster/endpoints/repository/*`
  - dispatcher/cache 条目需要保存远端服务状态。
  - 路由选择方法过滤非 Ready 服务。
  - PID 精确查找允许 Running 服务。

---

## 4. 问题二：服务集群可见性策略

### 4.1 当前问题

当前私有服务判断过于隐式：

```go
func (m *MethodMgr) IsPrivate() bool {
    return m.rpcCnt == 0
}
```

这表示只要服务注册了 RPC 方法，就会被视为集群可发布服务。更早的约定里，只有 `api` 开头的接口服务不主动注册到集群；但一旦引入显式可见性，这类前缀规则也应该退出“服务发现决策”。对于“玩家服务”“场景内实体服务”“房间对象服务”等数量巨大的实体服务，如果继续依赖方法前缀推断可见性，会导致：

- etcd key 数量随实体数量增长。
- 所有节点 watch 和缓存大量并不需要全局发现的服务。
- 普通服务路由候选集过大。
- 实体上下线造成集群事件风暴。

### 4.2 新可见性模型

新增显式可见性枚举，建议放在 `engine/pkg/def`：

```go
type ServiceVisibility int32

const (
    ServiceVisibilityAuto ServiceVisibility = iota
    ServiceVisibilityCluster
    ServiceVisibilityNode
    ServiceVisibilityPrivate
)
```

语义：

| 可见性 | etcd 发布 | 集群普通路由可发现 | 持有 PID 可远程访问 | 典型服务 |
| -------- | ----------- | -------------------- | --------------------- | ---------- |
| Auto | 按兼容规则推断 | 按兼容规则推断 | 按推断结果 | 旧配置迁移模式 |
| Cluster | 是 | 是，且 Ready 后可选中 | 是 | API、匹配、全局聊天、公共网关 |
| Node | 否 | 否 | 是 | 玩家、房间、场景实体 |
| Private | 否 | 否 | 否或仅本进程内部 | DB 内部模块、工具服务、纯内部组件 |

### 4.3 Auto 与迁移规则

最终目标是：服务是否私有、是否集群可见、是否允许远端定向调用，全部由 `visibility` 显式定义。`rpcCnt`、`api` 前缀、`rpc` 前缀不再参与服务级可见性判断。

配置中未声明 `visibility` 时，默认值应为 `private`。这是新的安全默认值：服务只有在显式声明 `cluster` 或 `node` 后，才会获得集群发布或远端定向调用能力。

为了降低迁移成本，短期可以保留 `Auto` 作为显式兼容模式：

```text
if rpcCnt == 0:
    visibility = Private
else:
    visibility = Cluster
```

该规则只用于明确配置 `visibility: auto` 的旧服务。新服务和逐步迁移后的服务应显式声明 `cluster`、`node` 或 `private`，并避免继续依赖 RPC/API 方法前缀推断服务发现行为。

业务可以显式配置：

```yaml
services:
  - name: PlayerService
    visibility: node

  - name: MatchService
    visibility: cluster

  - name: DBProxy
    visibility: private

   - name: LegacyService
      visibility: auto
```

如果省略 `visibility`：

```yaml
services:
   - name: InternalCacheService
      # visibility 默认为 private
```

### 4.4 Node 可见性的访问模型

`ServiceVisibilityNode` 的核心是“不参与发现，但允许持有 PID 的定向访问”。

典型流程：

```text
1. PlayerService 启动，visibility=node
2. 服务只加入本节点 Repository，不发布到 etcd
3. 玩家进入场景时，将 Player PID 交给 SceneService
4. SceneService 持有完整 PID 后发起定向 RPC
5. 本地 Repository 找不到该 PID 时，EndpointManager.GetDispatcher() 使用 AddTmp 创建临时 dispatcher
6. 临时 dispatcher 根据 PID 中的 nodeUid/rpcType/address 走远程发送链路
7. 目标节点收到请求后，通过本地 Repository 找到 PlayerService mailbox 并投递
```

这个模型不要求每个 PlayerService 出现在 etcd 中，集群发现规模只取决于 Cluster 可见服务数量。

### 4.5 Private 与 Node 的边界

需要明确区分：

- `Node`：不被发现，但拥有 PID 的远端服务可以调用。
- `Private`：不被发现，也不应允许远端定向调用。

因此远端入口在投递到本地服务前，需要能够判断目标服务是否允许远端访问。建议增加：

```go
GetVisibility() def.ServiceVisibility
IsRemoteCallable() bool
```

其中：

```text
Cluster -> remote callable
Node    -> remote callable
Private -> not remote callable
```

`IsPrivate()` 可以短期保留为兼容 API，但内部应改为基于 visibility 判断。迁移完成后，`MethodMgr.IsPrivate()` 和 `rpcCnt` 不应再作为服务私有性的来源；`rpcCnt` 最多只保留为方法表统计或兼容诊断信息。

### 4.6 主从模式关系

当前 `Service.Start()` 中有逻辑：

```go
if !s.isPrimarySecondaryMode || s.IsPrivate() || !isCluster {
    s.pid.SetMaster(true)
}
```

引入 visibility 后建议调整为：

```text
if !primarySecondaryMode || visibility != Cluster || !clusterMode:
    SetMaster(true)
```

即只有 `Cluster` 可见且启用主从的服务才需要参与跨节点主从选择；`Node` 和 `Private` 服务默认本地 master。

---

## 5. 推荐实现阶段

### 阶段 1：生命周期 Ready 状态与两阶段更新

1. **新增 Ready 状态**
   - 文件：`engine/pkg/def/consts.go`
   - 操作：插入 `SvcStatusReady`，调整关闭状态顺序。
   - 风险：中。所有基于数值比较的状态判断都需要复查。

2. **调整 Service.Start()**
   - 文件：`engine/pkg/core/service.go`
   - 操作：`OnStart()` 后设为 Running 并注册；`OnStarted()` 成功后设为 Ready 并更新集群。
   - 风险：中。失败回滚必须完整，避免 Running 残留。

3. **新增端点状态更新接口**
   - 文件：`engine/pkg/interfaces/INodeContext.go`
   - 文件：`engine/pkg/cluster/endpoints/endpoints.go`
   - 操作：新增 `ServiceReady(svc IService)` 或通用 `UpdateServiceStatus(svc IService, status int32)`。
   - 风险：低。接口改动会影响 NodeContext 实现和测试桩。

4. **etcd value 携带状态**
   - 文件：`engine/pkg/cluster/discovery/etcd/*`
   - 操作：注册和 Ready 更新都写入同一服务 key，但 value 中状态不同。
   - 风险：中。需要兼容旧 value 或一次性迁移。

5. **Repository 路由过滤**
   - 文件：`engine/pkg/cluster/endpoints/repository/*`
   - 操作：普通选择只返回 Ready；PID 精确查找允许 Running/Ready。
   - 风险：中。需要梳理所有 Select 方法，避免漏掉某条路由路径。

### 阶段 2：服务可见性策略

1. **新增 ServiceVisibility 类型**
   - 文件：`engine/pkg/def/*`
   - 操作：定义 `Auto/Cluster/Node/Private`。
   - 风险：低。

2. **配置层支持 visibility**
   - 文件：`engine/pkg/config/*`
   - 操作：在服务初始化配置中增加 `visibility` 字段，解析字符串到枚举；字段缺省时使用 `private`。
   - 风险：中。旧服务若依赖隐式 RPC 注册，需要显式补充 `visibility: auto` 或 `visibility: cluster`。

3. **解除 RPC/API 前缀与服务可见性的绑定**
   - 文件：`engine/pkg/core/rpc/handler.go`
   - 文件：`engine/pkg/core/service.go`
   - 操作：`MethodMgr.IsPrivate()` 不再作为服务注册依据；服务级私有性改为读取 `ServiceVisibility`。
   - 风险：中。需要提供显式 `Auto` 兼容模式，并在迁移文档中提示旧服务补配置。

4. **Service 保存并暴露 visibility**
   - 文件：`engine/pkg/core/service.go`
   - 文件：`engine/pkg/interfaces/IService.go`
   - 操作：增加字段和 `GetVisibility()`；`IsPrivate()` 改为兼容包装。
   - 风险：中。接口变更影响测试 mock。

5. **EndpointManager 按 visibility 发布**
   - 文件：`engine/pkg/cluster/endpoints/endpoints.go`
   - 操作：只有 `Cluster` 可见服务发布到 etcd；`Node/Private` 只注册本地。
   - 风险：中。需要验证定向调用路径仍可到达 Node 服务。

6. **远端投递保护 Private 服务**
   - 文件：`engine/pkg/rpc/remote/*` 或远端请求落地入口
   - 操作：远端请求投递本地 mailbox 前检查目标服务 visibility，拒绝 Private。
   - 风险：中。需要确认 reply、内部系统消息不会被误拒。

### 阶段 3：测试与文档同步

1. **生命周期测试**
   - 验证 Running 状态会进入缓存但不被普通路由选中。
   - 验证 Ready 后可选中。
   - 验证 `OnStarted()` 失败会 RemoveService。

2. **可见性测试**
   - 未配置 `visibility` 时默认推断为 Private。
   - 显式 `visibility=private` 时，即使存在 RPC 方法也不注册 etcd。
   - 显式 `visibility=cluster` 时，即使没有 RPC 方法也按集群服务发布，但普通路由是否可调用仍取决于实际方法表。
   - 显式 `visibility=auto` 且 `rpcCnt==0` 时推断为 Private。
   - 显式 `visibility=auto` 且 `rpcCnt>0` 时推断为 Cluster。
   - `Node` 不发布 etcd，但持有 PID 的远程调用可达。
   - `Private` 不发布 etcd，远程定向调用被拒绝。

3. **文档同步**
   - 更新 `docs/CONFIG_REFERENCE.md` 中服务配置示例。
   - 更新 `docs/CODEMAPS/actor.md` 或相关模块地图中的生命周期描述。

---

## 6. 风险与缓解

### 风险：状态比较逻辑被 Ready 影响

当前存在类似 `status > SvcStatusRunning` 的判断。插入 Ready 后，这类判断可能误判 Ready 服务为 closed。

缓解：实现前全局搜索 `SvcStatusRunning`、`SvcStatusClosing`、`IsClosed()`，将关闭语义统一改为 `status >= SvcStatusClosing`。

### 风险：Running 缓存被普通路由误选中

如果某个 Repository 选择路径没有过滤 Ready，会重新出现原问题。

缓解：将过滤逻辑集中到 dispatcher 条目的统一候选判断方法，例如 `isSelectable()`，不要在每个 Select 方法中散落判断。

### 风险：etcd 重复 put 被当成重复 watcher

Ready 更新与 Running 注册使用同一 key。watch 处理逻辑需要识别“已存在服务的状态更新”，而不是报 watcher already exists。

缓解：远端 repository 更新路径设计为 upsert；本地注册 watcher 仍按本地服务维度管理，不应因 Ready 更新重复创建本地 watcher。

### 风险：Node 可见性绕过发现后缺少地址信息

如果 PID 中没有足够的 node/rpc/address 信息，临时 dispatcher 可能无法建立远程连接。

缓解：确认 PID 创建时保留 `nodeUid`、`rpcType`、必要地址或能通过 nodeUid 找到 node 级连接。若缺少，应补充“节点级发现”和“服务级发现”的分层设计。

### 风险：Private 远端拒绝误伤系统消息

Private 服务可能仍需要本进程内部消息，但不应暴露给远端。

缓解：拒绝逻辑放在远端请求入口，不影响本地 mailbox 投递；系统内部消息需要明确来源和绕行规则。

---

## 7. 成功标准

- [ ] Service 启动期间会先以 Running 状态进入集群缓存。
- [ ] Running 服务不会被普通服务发现和负载均衡选中。
- [ ] `OnStarted()` 成功后服务更新为 Ready，并开始参与普通路由选择。
- [ ] `OnStarted()` 失败时，Running 注册会被主动撤销。
- [ ] 服务可见性支持 `Auto/Cluster/Node/Private`。
- [ ] 显式 `visibility` 优先级高于 RPC/API 方法前缀。
- [ ] 配置未声明 `visibility` 时默认使用 Private。
- [ ] Auto 仅作为显式配置的旧服务迁移兼容逻辑。
- [ ] Node 可见服务不注册 etcd，但持有 PID 时可远程访问。
- [ ] Private 服务不注册 etcd，且远端定向访问会被拒绝。
- [ ] etcd/watch/repository 能正确处理同一服务 key 的 Running -> Ready 更新。
- [ ] `go test ./...` 或至少相关包测试通过。

---

## 8. 推荐结论

建议采用“两阶段集群更新 + 显式服务可见性”的组合方案。

生命周期上，Running 表示“服务已进入集群缓存，可支持启动期定向交互”，Ready 表示“服务业务完全就绪，可被普通路由选中”。

可见性上，Cluster/Node/Private 将“全局发现”和“持有 PID 的访问能力”拆开：公共服务继续注册到 etcd；玩家、场景实体等高基数服务使用 Node 可见性；纯内部服务使用 Private。这样可以控制服务发现规模，同时保留业务中必要的定向调用能力。
---

## 9. 实施约束与兼容性说明

### 9.1 状态码二进制兼容

`SvcStatusReady` 插入在 `SvcStatusRunning` 之后，导致 `Closing/Closed/Retire` 的整型值各 +1：

```text
旧: Unknown=0, Init=1, Starting=2, Running=3, Closing=4, Closed=5, Retire=6
新: Unknown=0, Init=1, Starting=2, Running=3, Ready=4, Closing=5, Closed=6, Retire=7
```

etcd 持久化的 `ServiceEntry.Status` 以 `int32` 存储。滚动升级期间，新旧节点对同一数值有不同语义。

**实际影响评估**：节点仅在 Running/Ready 两个阶段向 etcd 写入 Status（3 或 4）。关闭流程走 key 删除（Dis），不写 Closing/Closed。因此：
- 旧节点写 `Status=3`（旧 Running）→ 新节点读到 3 = 新 Running（含义一致）。
- 旧节点不会写 `Status=4`（旧为 Closing），因为关闭走删除。
- 新节点写 `Status=4`（Ready）→ 旧节点对该字段无逻辑（旧代码无 Status 解析），忽略。

**结论**：滚动升级安全。但要求：
1. 永远不能在 etcd Put 中写入 `Closing/Closed/Retire` 状态。
2. 如果未来增加新状态值，必须验证旧节点解析兼容性。

### 9.2 失败回滚为阻塞操作

`rollbackStart()` 调用 `mailbox.Stop()`（BeginStop + Wait），会阻塞等待所有 mailbox worker 退出。这是有意为之的安全设计：确保回滚后 mailbox 不再处理消息，避免对已释放资源的悬空访问。

**约束**：`OnStart()` 和 `OnStarted()` 实现不应在内部阻塞等待本服务 mailbox 投递完成（否则回滚时形成死锁）。若需要与 mailbox 的 worker 交互，应通过异步通道或独立 goroutine。

### 9.3 OnRelease 必须对未完全初始化状态幂等

`rollbackStart()` 内部调用 `releaseWithEndpoint(false)`，该路径最终执行 `OnRelease()`。当 `OnStart()` 在用户代码初始化资源前失败时，`OnRelease()` 仍会被调用。

**契约**：所有 `IService` 实现的 `OnRelease()` 必须能够安全处理"资源尚未创建"的情况。推荐做法：
- 在释放资源前检查 nil（`if db != nil { db.Close() }`）。
- 不要在 `OnRelease()` 中假设 `OnStart()` 已成功执行。

### 9.4 AddService → OnStarted 失败 → RemoveService 的短暂抖动

启动流程中 `em.AddService(s)` 同步触发 etcd 注册事件。如果随后 `OnStarted()` 失败，回滚会触发 `em.RemoveService(s)` 发布下线事件。远端节点会观察到 Add → Delete 抖动。

**影响评估**：因为 Add 阶段服务状态为 Running，远端路由不会选中该服务，因此抖动对业务无影响。仅 etcd watch 日志会记录一条短暂注册/注销序列。

### 9.5 SetTmpMapStrategy 的线程安全

`Repository.tmpMapStrategy` 使用 `atomic.Value` 存储。调用方必须遵守：
- 传入值类型必须为 `func(*tmpInfo) bool`（不能为 nil 的 typed nil）。
- 如果需要恢复默认策略，传入 `nil`（untyped），`tick` goroutine 会 fallback 到 `defaultStrategy`。
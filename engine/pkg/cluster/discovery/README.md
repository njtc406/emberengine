# 服务发现说明

本模块提供统一的服务发现抽象，后端实现（如 etcd）通过适配层接入，应用与集群仅依赖统一接口，运行时可插拔。

## 架构概览
- 统一接口位于包 `cluster/discovery`：
  - `IDiscoveryServiceWatcher`：启动前缀监听与初始同步（Start）。
  - `IDiscoveryHealthMonitor`：健康检测（Start/Stop）。
  - `ILeaseManager`：租约管理（Grant/Revoke/KeepAliveLoop）。
  - `IServiceRegistry`：服务注册（ServiceKey/MasterKey/RegisterService）。
  - `IMasterElection`：主选举（TryAcquireMaster）。
  - `IClientProvider`：统一客户端适配（IsConnected/WatchPrefix/Watch/GetPrefix）。
- 后端实现置于子包（例如 `cluster/discovery/etcd/`），通过 `init()` 调用 `Register(name, impl)` 注册。
- 集群层通过 `CreateDiscovery(conf.DiscoveryType)` 获取实现，运行时根据配置选择后端。

## etcd 适配实现
- 聚合器 `EtcdDiscovery` 组合组件：
  - `EtcdServiceWatcher`：封装 `watchLoop` 与初始状态同步。
  - `EtcdHealthMonitor`：定期检测连接状态并触发重连与恢复。
  - `etcdClientProvider`：封装 watch/get/连接状态，供内部统一使用。
  - `etcdLeaseManager`：租约的申请、撤销与保活循环。
  - `etcdServiceRegistry`：服务键路径与注册写入（绑定租约）。
  - `etcdMasterElection`：使用事务 `CreateRevision(masterKey)==0` + 绑定租约的抢主逻辑。
- 事件管线：后端监听 etcd 事件并转换为框架事件 (`SysEventETCDPut` / `SysEventETCDDel`)，推送到集群的事件处理器；`Cluster.run` 仅分发，不再判断具体中间件事件类型。

## 主从选举与一致性
- 抢主：事务条件确保并发中只有一个成功，失败者保持从并监听主键。
- 绑定租约：主键随主实例租约失效自动清理；从实例监听到删除后再发起竞选，避免脑裂。
- 连接判断：所有路径使用 `provider.IsConnected()` 统一校验；连接异常时重建 watch 通道与触发重连。

## 使用方式（应用/集群）
1. 在集群入口空导入后端包，以触发注册：
   - 例如在 `cluster.go`：`_ "github.com/njtc406/emberengine/engine/pkg/cluster/discovery/etcd"`。
2. 初始化：集群调用 `discovery.CreateDiscovery(DiscoveryType)`，并执行 `Init(proc, conf) / Start()`。
3. 事件：后端推送框架事件到集群的 `eventProcessor`，集群只做分发。

## 扩展新中间件
- 在新子包实现上述接口（如 `redis/`、`zk/`），内部提供对应组件：ClientProvider、LeaseManager、ServiceRegistry、MasterElection、Watcher、HealthMonitor。
- 在新后端包的 `init()` 中调用 `Register("redis", NewRedisDiscovery())` 注册。
- 在集群入口空导入新后端包；配置中将 `DiscoveryType` 切换为对应名称即可。

## 测试与注入
- 通过 `IClientProvider` 可在单元测试中注入 mock，验证 watch、初始同步、健康检测与重连路径。
- 示例：`discovery/etcd/discovery_test.go` 使用 `mockProvider` 触发 `watchLoop` 与 `syncInitialState`，断言事件被推送。

## 约束与注意
- TTL 建议保持较小（默认 3s），加快主键失效后的收敛。
- 外部系统不应直接写不带租约的主键；必要时可在注册值中存 `ServiceUid` 并在监听侧做校验与报警。
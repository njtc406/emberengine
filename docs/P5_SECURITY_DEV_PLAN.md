# Phase 5：生产化能力 — 安全底座开发计划

> 创建时间：2026-05-14
> 关联文档：[ROADMAP.md](ROADMAP.md) / [NEXT_GOALS.md](NEXT_GOALS.md)

---

## 一、目标

为 EmberEngine 补齐生产环境必需的安全基础能力，分阶段实施：

1. **P5-1 gRPC mTLS**：节点间 gRPC 通信强制双向 TLS 认证
2. **P5-2 NATS TLS**：事件总线 NATS 连接启用 TLS（配置已就位，需实际接线）
3. **P5-3 TLS 工具 + 测试**：证书加载工具函数 + 单元测试
4. **P5-4 全量验证 + 文档回填**
5. **P5-5~P5-9 RBAC 授权引擎**：Principal、Role/Permission/Binding、RPC Handler 拦截、单元测试与文档回填
6. **P5-10~P5-15 策略存储与分发**：本地策略文件、etcd snapshot/watch、Authorizer 原子热更新

> **后续阶段**（本轮不实施，记录为待办）：
> - 策略存储与分发（本地策略 + etcd watch）
> - 审计日志
> - 灰度路由（Endpoint metadata + version/weight 路由）
> - 插件系统（PluginManager 生命周期）

---

## 二、P5 任务状态

| 编号 | 任务 | 当前状态 | 处理方式 |
|------|------|----------|----------|
| P5-1 | gRPC mTLS 支持 | ✅ 已完成 | server.go 加载 TLS credentials，client 支持 *tls.Config |
| P5-2 | NATS TLS 连接完善 | ✅ 已完成 | NATS client sender 集成 tlsx.LoadClientTLS |
| P5-3 | TLS 工具 + 测试 | ✅ 已完成 | utils/tlsx/ 包，12 tests（含 mTLS 握手集成测试） |
| P5-4 | mTLS 全量验证 + 文档回填 | ✅ 已完成 | build/vet/test 全绿，ROADMAP/NEXT_GOALS 已更新 |
| P5-5 | authz 包：Principal + RBAC 引擎 | ✅ 已完成 | Principal 身份模型 + Authorizer(Role/Permission/Binding) + 通配符匹配 |
| P5-6 | PID 身份提取 | ✅ 已完成 | PrincipalFromPID 从 actor.PID 提取 ServiceType/ServiceName/NodeUid |
| P5-7 | RPC Handler 授权拦截 | ✅ 已完成 | HandleRequest 方法分发前执行 RBAC 检查 |
| P5-8 | authz 单元测试 | ✅ 已完成 | 24 tests 全通过（Principal/Enable/Wildcard/Prefix/Exact/Multi-role/Remove/Unbind） |
| P5-9 | RBAC 全量验证 + 文档回填 | ✅ 已完成 | go test ./... 全绿，ROADMAP/NEXT_GOALS/P5_PLAN 已更新 |
| P5-10 | 策略模型与 Authorizer 快照 | 📋 已规划 | PolicySnapshot + ApplySnapshot 原子替换 |
| P5-11 | 本地策略文件加载 | 📋 已规划 | LocalPolicyStore，单机/测试最小可用路径 |
| P5-12 | PolicyWatcher 生命周期 | 📋 已规划 | 初始加载、watch 更新、Stop 幂等、错误保留旧快照 |
| P5-13 | etcd 策略存储与分发 | 📋 已规划 | snapshot 路径 + prefix watch + revision 单调更新 |
| P5-14 | 配置模板与测试 | 📋 已规划 | AuthzConf、模板默认 disabled、fake store/kv 测试 |
| P5-15 | 全量验证 + 文档回填 | 📋 已规划 | build/vet/test 全绿，ROADMAP/NEXT_GOALS/CONFIG_REFERENCE 回填 |

> P5-10~P5-15 详细拆分见：[P5_POLICY_DISTRIBUTION_DEV_PLAN.md](P5_POLICY_DISTRIBUTION_DEV_PLAN.md)

---

## 三、现有基础

### 3.1 配置结构已就位

- `RPCServer` 已有 `Cert`, `CertKey`, `CAs` 字段（define.go L90-92）
- `NatsConf` 已有 `Cert`, `CertKey`, `CAs`, `TLSServerName`, `InsecureSkipVerify`（define.go L305-315）

### 3.2 当前代码的 TLS 空缺

| 文件 | 问题 |
|------|------|
| `rpc/remote/gr/server.go` | `grpc.NewServer()` 无 TLS option |
| `rpc/client/sender_remote_grpc.go` | 硬编码 `insecure.NewCredentials()` |
| NATS 连接器 | TLS 配置字段存在但未使用 |

---

## 四、实施方案

### P5-1 gRPC mTLS

**server.go 改造**：
```go
// 如果配置了证书，创建 TLS server
if conf.Cert != "" && conf.CertKey != "" {
    tlsConfig, err := tlsx.LoadServerTLS(conf.Cert, conf.CertKey, conf.CAs)
    creds := credentials.NewTLS(tlsConfig)
    s.server = grpc.NewServer(grpc.Creds(creds))
} else {
    s.server = grpc.NewServer() // 开发模式无 TLS
}
```

**sender_remote_grpc.go 改造**：
```go
// 如果有 TLS 配置，使用 mTLS
var creds credentials.TransportCredentials
if tlsConf != nil {
    creds = credentials.NewTLS(tlsConf)
} else {
    creds = insecure.NewCredentials()
}
conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(creds))
```

### P5-2 NATS TLS

在 NATS 连接创建处根据 `NatsConf` 加载 TLS 配置。

### P5-3 TLS 工具

新增 `engine/pkg/utils/tlsx/` 包：
```go
// LoadServerTLS 加载服务端 mTLS 配置
func LoadServerTLS(certFile, keyFile, caFile string) (*tls.Config, error)

// LoadClientTLS 加载客户端 mTLS 配置
func LoadClientTLS(certFile, keyFile, caFile, serverName string) (*tls.Config, error)
```

---

## 五、验证清单

- [x] `go build ./...` 通过
- [x] `go vet ./...` 零告警
- [x] `go test ./...` 全量通过
- [x] TLS 工具函数有完整单元测试（12 tests）
- [x] RBAC 授权引擎有完整单元测试（24 tests）
- [x] 无 TLS 配置时行为不变（向后兼容）
- [x] RBAC 未启用时行为不变（默认禁用）
- [x] ROADMAP/NEXT_GOALS 状态更新

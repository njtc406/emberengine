# P4 示例、模板、文档产品化开发文档

> 创建时间：2026年5月14日  
> 来源：`docs/ROADMAP.md` Phase 4 + `docs/NEXT_GOALS.md` Phase A 文档待补充清单  
> 前置条件：P0/P1/P2/P3 已完成，`go build/vet/test` 全绿

---

## 一、P4 总目标

P4 的核心目标是将 EmberEngine 从"可用"推进到"易用"：

1. 将现有压测/集群示例固化为可复用的回归资产
2. 生成权威配置参数说明表
3. 编写用户可执行的入门和开发文档

P4 不引入新功能和代码变更，纯文档/示例产出。

---

## 二、P4 任务状态

| 编号 | 任务 | 当前状态 | 处理方式 |
|------|------|----------|----------|
| P4-1 | 压测场景固化 | ✅ 已完成 | node_concurrency/README.md — 环境变量/指标/pprof 说明 |
| P4-2 | 集群场景固化 | ✅ 已完成 | example/ReadMe.md — 8 个示例总览 + 4 个启动场景 |
| P4-3 | 配置说明表 | ✅ 已完成 | CONFIG_REFERENCE.md — 22 节完整配置参数参考 |
| P4-4 | 快速开始指南 | ✅ 已完成 | QUICK_START.md — 从零到运行第一个 Service |
| P4-5 | Service 开发指南 | ✅ 已完成 | SERVICE_DEV_GUIDE.md — RPC/事件/定时器/Module/RW 分离 |
| P4-6 | 全量验证 + 文档回填 | ✅ 已完成 | ROADMAP/NEXT_GOALS 状态更新 |

---

## 三、开发原则

1. **不修改源码**：P4 仅产出文档和 README
2. **面向新用户**：假设读者从未接触过 EmberEngine
3. **可执行**：所有示例命令必须可以直接运行
4. **简洁精准**：配置说明表以表格为主，避免冗余叙述

---

## 四、P4-1：压测场景固化

### 产出

`example/node_concurrency/README.md`

### 内容

1. 场景说明（该示例做什么）
2. 前置条件（etcd、NATS 地址）
3. 环境变量表（BENCH_MODE/BENCH_TOTAL/BENCH_CONCURRENCY/BENCH_TYPE/BENCH_RECORD_DURATIONS 等）
4. 启动命令
5. 预期输出与关键指标解读（QPS、P99、errors）
6. 与 pprof 配合使用的方式

---

## 五、P4-2：集群场景固化

### 产出

`example/README.md`（重写，覆盖所有示例）

### 内容

1. 示例总览表（每个 node_* 是什么、演示什么）
2. 依赖说明（etcd、NATS）
3. 基础 RPC 示例（node1/2/3 启动顺序与验证方法）
4. 主从模式示例（node_master/slave 启动顺序、故障切换验证）
5. 并发测试示例（node_concurrency 快速入口）

---

## 六、P4-3：配置说明表

### 产出

`docs/CONFIG_REFERENCE.md`

### 内容

从 `engine/pkg/config/define.go` 和 `template/config/node.yaml` 提取，生成完整配置参数参考表。

表格格式：

| 配置路径 | 类型 | 默认值 | 必填 | 说明 |
|----------|------|--------|------|------|

按层级分节：
1. NodeConf（基础节点配置）
2. RpcMonitorConf（RPC 监控）
3. EventBusConf（事件总线 / NATS）
4. DeDuplicatorConf（去重器）
5. TimingWheelConf（定时器轮）
6. ClusterConf（集群 / etcd / RPC 服务器）
7. ServiceConf（服务列表 / 启动配置）
8. MailboxConf（邮箱 / 队列 / 调度策略）
9. MiddlewareConf（限流 / 熔断 / 统计）
10. ServiceLogConf（服务日志）
11. SystemLogger（系统日志）

---

## 七、P4-4：快速开始指南

### 产出

`docs/QUICK_START.md`

### 内容

1. 环境准备（Go 1.24+、etcd、NATS 可选）
2. 获取框架
3. 创建最小 Service（完整代码）
4. 编写配置文件
5. 启动并验证
6. 下一步（指向 Service 开发指南）

---

## 八、P4-5：Service 开发指南

### 产出

`docs/SERVICE_DEV_GUIDE.md`

### 内容

1. Service 生命周期（Init → Start → Started → Release）
2. 注册 RPC Handler（同步/异步/单向）
3. 调用其他 Service（Select → Call/AsyncCall/Send）
4. 使用 Module
5. 事件订阅与发布
6. 定时器
7. 读写分离模式
8. 配置自定义业务参数
9. 日志最佳实践

---

## 九、推荐实施顺序

```text
Step 1：P4-3 配置说明表（最高价值，降低配置心智负担）
Step 2：P4-4 快速开始指南（新用户第一个接触点）
Step 3：P4-5 Service 开发指南（开发者核心参考）
Step 4：P4-1 压测场景固化（回归资产）
Step 5：P4-2 集群场景固化（示例总览）
Step 6：P4-6 全量验证 + 文档回填
```

---

## 十、验证检查清单

- [ ] CONFIG_REFERENCE.md 覆盖 define.go 所有可配置字段
- [ ] QUICK_START.md 中的代码可以编译运行
- [ ] SERVICE_DEV_GUIDE.md 中的代码片段语法正确
- [ ] example/README.md 中的启动命令可执行
- [ ] node_concurrency/README.md 中的环境变量与代码一致
- [ ] ROADMAP.md Phase 4 状态更新
- [ ] NEXT_GOALS.md 文档清单更新

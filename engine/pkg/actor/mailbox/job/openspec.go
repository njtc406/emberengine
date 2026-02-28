// Package job 定义了 Mailbox 中的任务（Job）数据结构与工厂。
//
// # OpenSpec
//
//   - 模块:     邮箱任务
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/actor/mailbox/job
//   - 层级:     foundation
//   - 状态:     stable
//   - 线程安全: yes（Job 通过池化复用，获取和归还操作线程安全）
//
// # 概述
//
// job 包定义了投递到 Mailbox 中的最小执行单元 Job。每个 Job 携带
// 上下文、截止时间、优先级、分发键、类型和中间件上下文等元信息。
// JobFactory 提供池化的 Job 创建与回收机制，避免高频场景下的 GC 压力。
//
// # 核心类型
//
//   - Job:        实现 interfaces.IMailboxJob，封装单次消息处理的完整上下文。
//   - JobFactory: Job 对象池工厂，提供 Get/Put 操作。
//
// # 依赖
//
// 内部:
//   - interfaces: IMailboxJob 接口定义
package job

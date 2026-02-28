// Package pid 提供进程 PID 文件管理工具。
//
// # OpenSpec
//
//   - 模块:     PID 文件管理
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/utils/pid
//   - 层级:     utility
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// pid 包提供操作系统进程 PID 文件的创建、读取和清理工具。
// 用于守护进程场景下防止重复启动和进程管理。
//
// # 依赖
//
// 无外部依赖。
package pid

// Package errorlib 提供增强的错误处理库。
//
// # OpenSpec
//
//   - 模块:     错误处理库
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/utils/errorlib
//   - 层级:     utility
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// errorlib 包提供带调用栈信息的错误封装。在创建错误时自动捕获
// 调用栈（Caller），便于错误追溯和调试。
//
// # 核心类型
//
//   - Error:  增强型错误，携带调用位置信息。
//   - Caller: 调用栈位置信息提取工具。
//
// # 依赖
//
// 无外部依赖。
package errorlib

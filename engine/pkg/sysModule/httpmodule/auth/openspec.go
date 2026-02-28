// Package auth 提供 HTTP 模块的认证中间件。
//
// # OpenSpec
//
//   - 模块:     HTTP 认证
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/sysModule/httpmodule/auth
//   - 层级:     extension
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// auth 包提供 HTTP 请求的认证中间件实现。目前支持 Basic Auth
// 认证方式，可集成到 httpmodule 的中间件链中。
//
// # 核心类型
//
//   - BasicAuth: Basic Authentication 中间件实现。
//
// # 依赖
//
// 外部:
//   - 标准库 net/http
package auth

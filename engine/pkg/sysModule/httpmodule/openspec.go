// Package httpmodule 提供 HTTP 服务模块。
//
// # OpenSpec
//
//   - 模块:     HTTP 模块
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/sysModule/httpmodule
//   - 层级:     extension
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// httpmodule 包提供嵌入式 HTTP 服务能力。作为系统模块挂载到
// Service 中，可快速暴露 RESTful API 端点。支持路由注册、
// 中间件链和认证（通过 auth 子包）。
//
// # 核心类型
//
//   - HttpModule: HTTP 服务模块，嵌入 core.Module，管理 HTTP Server 生命周期。
//
// # 子包
//
//   - auth: 认证中间件（如 BasicAuth）。
//
// # 依赖
//
// 内部:
//   - core:   Module 基类
//   - config: HTTP 服务配置
//
// 外部:
//   - github.com/gin-gonic/gin: HTTP 框架（如使用）
package httpmodule

// Package router 提供系统级路由模块。
//
// # OpenSpec
//
//   - 模块:     系统路由模块
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/sysModule/router
//   - 层级:     extension
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// sysModule/router 包提供系统级别的路由功能模块。作为一个标准的
// 系统模块挂载到 Service 中，提供路由注册和请求分发的 API。
// 与 engine/pkg/router（核心路由器）不同，本包是面向业务层的
// 路由模块封装。
//
// # 核心类型
//
//   - RouterModule: 系统路由模块，嵌入 core.Module。
//
// # 依赖
//
// 内部:
//   - core:   Module 基类
//   - router: 核心路由器
package router

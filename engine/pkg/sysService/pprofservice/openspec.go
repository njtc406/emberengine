// Package pprofservice 提供内置的 pprof 性能调试服务。
//
// # OpenSpec
//
//   - 模块:     PProf 服务
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/sysService/pprofservice
//   - 层级:     extension
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// pprofservice 包提供内置的 Go pprof HTTP 端点。作为系统服务
// 注册到 Node 中，在配置启用时自动绑定 pprof 路由，
// 方便在线进行 CPU、内存、goroutine 等性能分析。
//
// # 核心类型
//
//   - PprofService: pprof 系统服务，嵌入 core.Service。
//
// # 子包
//
//   - config: pprof 服务配置（地址、端口等）。
//
// # 依赖
//
// 内部:
//   - core:   Service 基类
//   - config: 服务配置
//
// 外部:
//   - net/http/pprof: Go 标准库 pprof
package pprofservice

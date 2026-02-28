// Package validate 提供配置和数据校验引擎。
//
// # OpenSpec
//
//   - 模块:     校验引擎
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/utils/validate
//   - 层级:     utility
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// validate 包封装了结构体校验引擎，支持标签驱动的字段校验
// 和自定义校验规则注册。用于配置加载后的合法性检查。
//
// # 核心功能
//
//   - 标准校验引擎初始化。
//   - 自定义校验规则注册。
//
// # 依赖
//
// 外部:
//   - github.com/go-playground/validator/v10: 校验库
package validate

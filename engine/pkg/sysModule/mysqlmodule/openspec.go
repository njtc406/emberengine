// Package mysqlmodule 提供基于 GORM 的 MySQL 数据库连接模块。
//
// # OpenSpec
//
//   - 模块:     MySQL 模块
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/sysModule/mysqlmodule
//   - 层级:     extension
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// mysqlmodule 包封装了基于 GORM ORM 框架的 MySQL 数据库连接管理。
// 作为系统模块挂载到 Service 中，提供数据库 CRUD 操作的便捷入口。
// 支持连接池配置、慢查询日志等特性。
//
// # 核心类型
//
//   - MysqlModule: MySQL 连接模块，嵌入 core.Module，持有 GORM DB 实例。
//
// # 依赖
//
// 内部:
//   - core:   Module 基类
//   - config: 数据库配置
//
// 外部:
//   - gorm.io/gorm:          GORM ORM 框架
//   - gorm.io/driver/mysql:  MySQL 驱动
package mysqlmodule

# emberlog

当前实现已迁移为 **zap** 后端（保留原有 API 调用形态），并集成按时间切割与按级别路由到不同文件。

## 功能

1. 支持日志按时间切分、过期自动删除
2. 支持日志级别与按级别路由到不同文件
3. 支持输出调用者信息
4. 支持控制台级别颜色（仅非 Production 模式）
5. **Production 模式**：开启后输出改为 JSON（便于日志采集/检索）

## 配置（LoggerConf）

核心字段：

- `Production`：生产标识。为 `true` 时，日志输出为 **JSON**，同时会禁用 `Color`。
- `Dir`：日志目录（写文件时使用）。
- `PrefixName`：日志文件名前缀；为空表示不写文件（仅 stdout，或全部丢弃）。
- `Level`：最小日志级别（`panic|fatal|error|warn|info|debug|trace`）。
- `Stdout`：是否输出到标准输出。
- `Caller` / `FullCaller`：是否输出调用者信息、是否输出完整路径。
- `Color`：是否启用控制台颜色（仅非 Production）。
- `Rotation`：统一切割配置（`Every`、`MaxAge`、`Pattern`）。
- `Routing`：路由配置（`Routes`、`AsyncMode`）。

路由字段：

- `Routing.Routes`：每个 `LevelRoute` 指定 `Levels`（该 route 接收的级别集合）与 `Name`（文件名后缀）。

默认行为：

- 未配置 `Routing.Routes` 且 `PrefixName` 非空时，默认所有级别写入单文件。
- `Production=true` 时，stdout 与文件输出均为 JSON。

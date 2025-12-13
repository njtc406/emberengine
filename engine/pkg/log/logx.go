// Package log
// 模块名: 扩展日志
// 功能描述: 提供带固定字段的 LoggerX 封装
// 作者:  yr  2025/12/2 01:00
// 最后更新:  yr  2025/12/2 01:00
package log

// NewLoggerX 创建一个带固定字段的 Logger。
//
// 关键点:
//   - 直接调用底层 Logger 的 WithFields 返回新的 Logger(通常是 *Entry 实现 Logger 接口)；
//   - 不在这里覆写 Info/Debug 等方法, 这样最终调用者仍然是底层实现, caller 由底层 logger 计算；
//   - 不在 LoggerX 中持有可变 map, 避免在多 goroutine 场景下出现竞态。
func NewLoggerX(logger *Logger, fields Fields) ILoggerX {
	if logger == nil {
		return nil
	}
	if fields == nil {
		fields = Fields{}
	}
	return logger.WithFields(map[string]interface{}(fields))
}

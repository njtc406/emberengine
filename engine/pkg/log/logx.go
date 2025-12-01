// Package log
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/12/2 01:00
// 最后更新:  yr  2025/12/2 01:00
package log

// TODO 这个有问题，需要覆写接口才行，但是覆写接口，会导致打印的调用层级有问题，还需要看看怎么修改

// LoggerX 扩展日志对象,可以在日志中添加一些固定的字段
type LoggerX struct {
	ILogger
	fields Fields
}

func NewLoggerX(logger ILogger, fields Fields) ILogger {
	return &LoggerX{
		ILogger: logger,
		fields:  fields,
	}
}

func (logger *LoggerX) SetFields(fields Fields) ILogger {
	for k, v := range fields {
		logger.fields[k] = v
	}
	return logger
}

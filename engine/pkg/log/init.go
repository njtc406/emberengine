package log

// NewLogger 创建一个新的 Logger 实例（工厂函数）。
// 每个 Node 持有独立的 Logger，互不干扰。
func NewLogger(conf *LoggerConf, isDebug bool) (*Logger, error) {
	conf = fixConf(conf)
	conf.Stdout = conf.Stdout || isDebug
	return NewDefaultLogger(conf)
}

// ── 以下为临时全局兼容 ──
// SysLogger 是临时全局 logger，仅供尚未完成 Node 化改造的消费方使用。
// 后续 Phase 中会逐步移除所有对 SysLogger 的引用。
// Deprecated: 请通过 NodeContext.Logger() 获取。
var SysLogger *Logger

// SetSysLogger 由 Node.Start 调用，设置全局 SysLogger（临时兼容）。
// Deprecated: 后续 Phase 删除。
func SetSysLogger(l *Logger) { SysLogger = l }

// Init 已废弃。使用 NewLogger 替代。
// Deprecated: 后续 Phase 删除。
func Init(conf *LoggerConf, isDebug bool) {
	if SysLogger != nil {
		return
	}
	l, err := NewLogger(conf, isDebug)
	if err != nil {
		panic(err)
	}
	SysLogger = l
	SysLogger.Info("-------->system log init ok<---------")
}

// Close 已废弃。使用 Logger.Close() 替代。
// Deprecated: 后续 Phase 删除。
func Close() {
	if SysLogger != nil {
		SysLogger.Info("-------->system log release<---------")
		Release(SysLogger)
		SysLogger = nil
	}
}

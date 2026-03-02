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

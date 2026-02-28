package log

// NewLogger 创建一个新的 Logger 实例（工厂函数）。
// 每个 Node 持有独立的 Logger，互不干扰。
func NewLogger(conf *LoggerConf, isDebug bool) (*Logger, error) {
	conf = fixConf(conf)
	conf.Stdout = conf.Stdout || isDebug
	return NewDefaultLogger(conf)
}

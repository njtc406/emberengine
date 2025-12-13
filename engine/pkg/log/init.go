package log

var SysLogger *Logger

func Init(conf *LoggerConf, isDebug bool) {
	if SysLogger != nil {
		return
	}
	conf = fixConf(conf)
	conf.Stdout = conf.Stdout || isDebug
	// Debug mode 默认更详细
	if isDebug && conf.Level == "info" {
		conf.Level = "debug"
	}
	logger, err := NewDefaultLogger(
		conf,
	)
	if err != nil {
		panic(err)
	}

	SysLogger = logger

	SysLogger.Info("-------->system log init ok<---------")
}

func Close() {
	if SysLogger != nil {
		SysLogger.Info("-------->system log release<---------")
		Release(SysLogger)
		SysLogger = nil
	}
}

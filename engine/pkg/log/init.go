package log

var SysLogger *Logger

func Init(conf *LoggerConf, isDebug bool) {
	if SysLogger != nil {
		return
	}
	conf = fixConf(conf)
	conf.Stdout = conf.Stdout || isDebug
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

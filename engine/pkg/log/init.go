package log

var SysLogger ILogger

func Init(conf *LoggerConf, isDebug bool) {
	if SysLogger != nil {
		return
	}
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
	SysLogger.Info("-------->system log release<---------")
}

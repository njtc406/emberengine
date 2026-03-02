package msgenvelope

var runtimeDebug bool

func SetDebug(enabled bool) {
	runtimeDebug = enabled
}

func isDebug() bool {
	return runtimeDebug
}

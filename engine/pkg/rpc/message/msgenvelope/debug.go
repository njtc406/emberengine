package msgenvelope

import "sync/atomic"

var runtimeDebug atomic.Bool

func SetDebug(enabled bool) {
	runtimeDebug.Store(enabled)
}

func isDebug() bool {
	return runtimeDebug.Load()
}

package diag

import (
	"os"
	"sync"
)

var enabledOnce sync.Once
var enabledCached bool

// Enabled reports whether optional diagnostic logs / instrumentation are enabled.
//
// Keep it strictly opt-in and cached to avoid per-message os.Getenv overhead,
// which can be significant on Windows.
func Enabled() bool {
	enabledOnce.Do(func() {
		enabledCached = os.Getenv("EMBER_DIAG") == "1"
	})
	return enabledCached
}

//go:build !windows && !linux && !darwin && !freebsd && !netbsd && !openbsd && !dragonfly

package perftest

import (
	"fmt"
	"runtime"
	"time"
)

func CPUTime() (time.Duration, error) {
	return 0, fmt.Errorf("process CPU helper unsupported on %s", runtime.GOOS)
}

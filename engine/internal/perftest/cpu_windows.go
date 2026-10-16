//go:build windows

package perftest

import (
	"golang.org/x/sys/windows"
	"time"
)

func CPUTime() (time.Duration, error) {
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(windows.CurrentProcess(), &creation, &exit, &kernel, &user); err != nil {
		return 0, err
	}
	ticks := func(f windows.Filetime) uint64 { return uint64(f.HighDateTime)<<32 | uint64(f.LowDateTime) }
	return time.Duration(ticks(kernel)+ticks(user)) * 100 * time.Nanosecond, nil
}

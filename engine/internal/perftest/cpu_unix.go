//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package perftest

import (
	"golang.org/x/sys/unix"
	"time"
)

func CPUTime() (time.Duration, error) {
	var r unix.Rusage
	if err := unix.Getrusage(unix.RUSAGE_SELF, &r); err != nil {
		return 0, err
	}
	return time.Duration(r.Utime.Sec+r.Stime.Sec)*time.Second + time.Duration(r.Utime.Usec+r.Stime.Usec)*time.Microsecond, nil
}

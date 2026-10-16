//go:build !windows

package perftest

import "time"

var clockOrigin = time.Now()

func Now() time.Duration                      { return time.Since(clockOrigin) }
func Since(start time.Duration) time.Duration { return Now() - start }

//go:build windows

package perftest

import (
	"golang.org/x/sys/windows"
	"time"
	"unsafe"
)

var counterProc = windows.NewLazySystemDLL("kernel32.dll").NewProc("QueryPerformanceCounter")
var counterFrequency = func() int64 {
	var frequency int64
	r, _, err := windows.NewLazySystemDLL("kernel32.dll").NewProc("QueryPerformanceFrequency").Call(uintptr(unsafe.Pointer(&frequency)))
	if r == 0 || frequency <= 0 {
		panic(err)
	}
	return frequency
}()

// Now uses QPC rather than Go 1.24's coarse Windows interrupt-time clock.
// Values are process-independent monotonic durations, not wall-clock timestamps.
func Now() time.Duration {
	var counter int64
	r, _, err := counterProc.Call(uintptr(unsafe.Pointer(&counter)))
	if r == 0 {
		panic(err)
	}
	return time.Duration(counter/counterFrequency)*time.Second + time.Duration(counter%counterFrequency)*time.Second/time.Duration(counterFrequency)
}

func Since(start time.Duration) time.Duration { return Now() - start }

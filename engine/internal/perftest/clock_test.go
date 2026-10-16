package perftest

import (
	"runtime"
	"testing"
	"time"
)

// A sub-millisecond clock must distinguish a fixed, observable arithmetic workload.
func TestClockResolvesShortWork(t *testing.T) {
	positive := 0
	var x uint64 = 1
	for i := 0; i < 1000; i++ {
		start := Now()
		for k := 0; k < 64; k++ {
			x = x*6364136223846793005 + 1
		}
		if Since(start) > 0 {
			positive++
		}
	}
	runtime.KeepAlive(x)
	if positive < 990 {
		t.Fatalf("short-work clock resolved only %d/1000 samples", positive)
	}
}

func TestCPUTimeMeasuresBusyWork(t *testing.T) {
	before, err := CPUTime()
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	var x uint64 = 1
	for time.Since(start) < 500*time.Millisecond {
		for k := 0; k < 10000; k++ {
			x = x*6364136223846793005 + 1
		}
	}
	runtime.KeepAlive(x)
	after, err := CPUTime()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("busy process CPU=%s wall=%s", after-before, time.Since(start))
	if after <= before {
		t.Fatal("CPU clock did not advance under sustained CPU work")
	}
}

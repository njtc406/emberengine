package perftest

import (
	"testing"
	"time"
)

func TestPercentiles(t *testing.T) {
	if got := Percentiles(nil); got != (Quantiles{}) {
		t.Fatal(got)
	}
	values := make([]time.Duration, 1000)
	for i := range values {
		values[i] = time.Duration(1000 - i)
	}
	if got := Percentiles(values); got != (Quantiles{500, 990, 999}) {
		t.Fatal(got)
	}
	if got := Percentiles([]time.Duration{7}); got != (Quantiles{7, 7, 7}) {
		t.Fatal(got)
	}
}
func TestCPUTimeMonotonic(t *testing.T) {
	before, err := CPUTime()
	if err != nil {
		t.Fatal(err)
	}
	after, err := CPUTime()
	if err != nil {
		t.Fatal(err)
	}
	if after < before {
		t.Fatalf("CPU regressed: %s -> %s", before, after)
	}
}

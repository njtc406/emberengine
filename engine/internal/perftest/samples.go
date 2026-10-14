package perftest

import (
	"os"
	"runtime"
	"sort"
	"time"
)

type Quantiles struct{ P50, P99, P999 time.Duration }

func Percentiles(v []time.Duration) Quantiles {
	if len(v) == 0 {
		return Quantiles{}
	}
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	at := func(p int) time.Duration { return v[(len(v)*p+999)/1000-1] }
	return Quantiles{at(500), at(990), at(999)}
}

type Metadata struct {
	CPU, OS, Arch, Go, Commit string
	Procs                     int
}

func Machine() Metadata {
	return Metadata{os.Getenv("PERF_CPU_MODEL"), runtime.GOOS, runtime.GOARCH, runtime.Version(), os.Getenv("PERF_COMMIT"), runtime.GOMAXPROCS(0)}
}

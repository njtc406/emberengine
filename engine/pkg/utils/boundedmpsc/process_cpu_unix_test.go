//go:build !windows

package boundedmpsc

import (
	"github.com/njtc406/emberengine/engine/internal/perftest"
	"time"
)

func processCPUTime() (time.Duration, error) { return perftest.CPUTime() }

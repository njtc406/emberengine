//go:build !windows

package boundedmpsc

import (
	"github.com/njtc406/emberengine/v2/engine/internal/perftest"
	"time"
)

func processCPUTime() (time.Duration, error) { return perftest.CPUTime() }

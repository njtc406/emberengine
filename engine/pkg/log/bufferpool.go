package log

import (
	"bytes"
	"sync"

	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
)

var bufferPool pool.IPool[*bytes.Buffer]
var once sync.Once

func getBufferPool(isDebug bool) pool.IPool[*bytes.Buffer] {
	once.Do(func() {
		var recorder pool.IStatsRecorder
		if isDebug {
			recorder = pool.NewStatsRecorder("log_buffer")
		} else {
			recorder = pool.NewNoStatsRecorder()
		}
		bufferPool = pool.NewSyncPoolWrapper(
			func() *bytes.Buffer {
				return new(bytes.Buffer)
			},
			recorder,
			pool.WithReset(func(t *bytes.Buffer) {
				t.Reset()
			}),
		)
	})
	return bufferPool
}

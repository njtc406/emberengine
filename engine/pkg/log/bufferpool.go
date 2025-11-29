package log

import (
	"bytes"

	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
)

var bufferPool = pool.NewSyncPoolWrapper(
	func() *bytes.Buffer {
		return new(bytes.Buffer)
	},
	nil,
	pool.WithReset(func(t *bytes.Buffer) {
		t.Reset()
	}),
)

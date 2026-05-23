package core

import (
	"context"
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/actor/mailbox/job"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvokeJob_UsesNanosecondDeadline(t *testing.T) {
	registry := newJobHandlerRegistry()
	var gotDeadline time.Time

	registerJobHandler(registry, def.MailboxJobTypeRpc, func(ctx context.Context, _ inf.IEnvelope) error {
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		gotDeadline = deadline
		return nil
	})

	rpcJob := job.NewRpcJob()
	defer rpcJob.Release()

	expected := time.Now().Add(200 * time.Millisecond)
	rpcJob.SetDeadline(expected.UnixNano())

	err := registry.InvokeJob(context.Background(), rpcJob)
	require.NoError(t, err)
	assert.WithinDuration(t, expected, gotDeadline, 50*time.Millisecond)
}

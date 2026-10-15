package cluster

import (
	"context"
	"sync"
	"testing"

	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/stretchr/testify/require"
)

type testClusterEvent struct {
	ctx  context.Context
	data any
}

func (e testClusterEvent) GetEventType() def.EventType { return 0 }
func (e testClusterEvent) GetData() any                { return e.data }
func (e testClusterEvent) GetContext() context.Context { return e.ctx }

func newTestClusterWithShards(workerCount, queueSize int) *Cluster {
	c := &Cluster{
		closed:         make(chan struct{}),
		workerCount:    workerCount,
		shards:         make([]chan inf.IEvent, workerCount),
		eventProcessor: event.NewTrigger(),
	}
	c.eventProcessor.Init(nil)
	for i := range c.shards {
		c.shards[i] = make(chan inf.IEvent, queueSize)
		c.workerWg.Add(1)
		go c.shardWorker(i)
	}
	return c
}

func TestClusterClose_IdempotentAndRejectsPush(t *testing.T) {
	c := newTestClusterWithShards(1, 1)
	c.Close()
	require.NotPanics(t, func() { c.Close() })

	err := c.PushEvent(testClusterEvent{ctx: context.Background()})
	require.Error(t, err)
	require.Contains(t, err.Error(), "cluster closed")
}

func TestClusterClose_ConcurrentPushDoesNotPanic(t *testing.T) {
	c := newTestClusterWithShards(2, 0)
	evt := testClusterEvent{ctx: context.Background(), data: testKeyProvider{key: []byte("svc-a")}}

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = c.PushEvent(evt)
			}
		}()
	}

	c.Close()
	wg.Wait()
}

type testKeyProvider struct {
	key []byte
}

func (p testKeyProvider) GetKey() []byte { return p.key }

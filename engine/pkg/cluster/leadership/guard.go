package leadership

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/event"
)

// Guard provides a minimal, backend-agnostic leadership guard.
//
// It does NOT try to infer business semantics; instead it exposes:
// - IsLeader: whether the local node currently holds leadership
// - Epoch: a fencing token (monotonic) for the current leadership
// - Ctx: a context that is cancelled immediately when leadership is lost
//
// The guard is driven by framework events:
// - event.ServiceBecomeMaster
// - event.ServiceLoseMaster
// - event.ServiceBecomeSlaver
// - event.ServiceDisconnected
//
// The epoch is read from event headers:
// - def.MasterEpochKey
// - def.MasterPrevEpochKey
//
// Typical usage in a service:
//
//	g := leadership.NewGuard(context.Background())
//	// inside your event handler:
//	g.OnEvent(ev)
//	// for master-only work:
//	go func() {
//	    <-g.Ctx().Done() // stop when lose master
//	}()
//
// NOTE: This guard reduces "split-brain" damage by offering a consistent
// cancellation point and a fencing token, but side-effect fencing still
// requires the business to pass/check the epoch at the side-effect boundary.
type Guard struct {
	baseCtx context.Context

	leader atomic.Bool
	epoch  atomic.Int64

	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
}

func NewGuard(baseCtx context.Context) *Guard {
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	g := &Guard{baseCtx: baseCtx}
	// Start as "not leader": ctx is already cancelled.
	ctx, cancel := context.WithCancel(baseCtx)
	cancel()
	g.ctx = ctx
	g.cancel = cancel
	return g
}

// IsLeader returns whether the local node is currently leader.
func (g *Guard) IsLeader() bool { return g.leader.Load() }

// Epoch returns the current fencing token (0 if not leader/unknown).
func (g *Guard) Epoch() int64 { return g.epoch.Load() }

// Ctx returns a context that is active only while the node is leader.
// It is cancelled immediately when leadership is lost.
func (g *Guard) Ctx() context.Context {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.ctx
}

// OnEvent consumes a framework event and updates leadership state.
//
// It returns true if leadership state changed.
func (g *Guard) OnEvent(ctx context.Context, eventType def.EventType, stateData *actor.MasterStateData) bool {
	switch eventType {
	case event.ServiceBecomeMaster:
		return g.becomeLeader(stateData.GetNewEpoch())

	case event.ServiceLoseMaster, event.ServiceBecomeSlaver, event.ServiceDisconnected:
		return g.loseLeader()
	default:
		return false
	}
}

func (g *Guard) becomeLeader(newEpoch int64) bool {
	// Treat 0 as "unknown" but still valid for cancellation semantics.
	// If epoch does not change and we're already leader, keep current ctx.
	if g.IsLeader() && g.Epoch() == newEpoch {
		return false
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if g.cancel != nil {
		g.cancel()
	}
	ctx, cancel := context.WithCancel(g.baseCtx)
	g.ctx = ctx
	g.cancel = cancel

	g.epoch.Store(newEpoch)
	g.leader.Store(true)
	return true
}

func (g *Guard) loseLeader() bool {
	if !g.IsLeader() {
		return false
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	g.leader.Store(false)
	g.epoch.Store(0)
	if g.cancel != nil {
		g.cancel()
	}
	// Keep ctx/cancel as-is (cancelled). Next leader will replace them.
	return true
}

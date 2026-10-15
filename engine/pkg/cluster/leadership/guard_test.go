package leadership

import (
	"context"
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/event"
)

func TestGuard_BecomeAndLoseLeader(t *testing.T) {
	g := NewGuard(context.Background())
	if g.IsLeader() {
		t.Fatalf("expected initial IsLeader=false")
	}
	select {
	case <-g.Ctx().Done():
		// ok
	default:
		t.Fatalf("expected initial ctx to be cancelled")
	}

	if changed := g.OnEvent(context.Background(), event.ServiceBecomeMaster, &actor.MasterStateData{NewEpoch: 123}); !changed {
		t.Fatalf("expected state change on become master")
	}
	if !g.IsLeader() || g.Epoch() != 123 {
		t.Fatalf("expected leader epoch=123, got IsLeader=%v epoch=%d", g.IsLeader(), g.Epoch())
	}

	ctx1 := g.Ctx()
	select {
	case <-ctx1.Done():
		t.Fatalf("expected leader ctx to be active")
	default:
		// ok
	}

	if changed := g.OnEvent(context.Background(), event.ServiceLoseMaster, nil); !changed {
		t.Fatalf("expected state change on lose master")
	}
	if g.IsLeader() {
		t.Fatalf("expected IsLeader=false after lose master")
	}

	select {
	case <-ctx1.Done():
		// ok
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("expected previous leader ctx to be cancelled promptly")
	}
}

func TestGuard_NewEpochReplacesContext(t *testing.T) {
	g := NewGuard(context.Background())
	_ = g.OnEvent(context.Background(), event.ServiceBecomeMaster, &actor.MasterStateData{NewEpoch: 1})
	ctx1 := g.Ctx()

	if changed := g.OnEvent(context.Background(), event.ServiceBecomeMaster, &actor.MasterStateData{NewEpoch: 2}); !changed {
		t.Fatalf("expected state change on epoch update")
	}
	ctx2 := g.Ctx()
	if ctx1 == ctx2 {
		t.Fatalf("expected ctx to be replaced on new epoch")
	}

	select {
	case <-ctx1.Done():
		// ok
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("expected old ctx to be cancelled on epoch update")
	}

	select {
	case <-ctx2.Done():
		t.Fatalf("expected new ctx to be active")
	default:
		// ok
	}
}

package client

import (
	"context"
	"errors"
	"testing"

	inf "github.com/njtc406/emberengine/v2/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/v2/engine/pkg/rpc/client/pool"
)

type enhancedTestEnvelope struct {
	inf.IEnvelope
	releases int
}

func (e *enhancedTestEnvelope) Release() { e.releases++ }

type enhancedTestSender struct {
	err       error
	requests  int
	responses int
}

func (s *enhancedTestSender) DeliverRequest(ctx context.Context, _ inf.IRpcDispatcher, e inf.IEnvelope) error {
	if ctx == nil {
		panic("nil context")
	}
	s.requests++
	e.Release()
	return s.err
}
func (s *enhancedTestSender) DeliverResponse(ctx context.Context, _ inf.IRpcDispatcher, e inf.IEnvelope) error {
	if ctx == nil {
		panic("nil context")
	}
	s.responses++
	e.Release()
	return s.err
}
func (*enhancedTestSender) Close()         {}
func (*enhancedTestSender) IsClosed() bool { return false }

func TestEnhancedSenderDelivery(t *testing.T) {
	for _, reply := range []bool{false, true} {
		for _, sendErr := range []error{nil, errors.New("send failed")} {
			t.Run(testDeliveryName(reply, sendErr), func(t *testing.T) {
				pm := pool.NewPoolManager(nil)
				t.Cleanup(pm.Close)
				transport := &enhancedTestSender{err: sendErr}
				pm.RegisterCreator("test", func(string) inf.IRpcSender { return transport })
				conf := pool.DefaultPoolConfig()
				conf.InitialConnections = 1
				conf.MinConnections = 1
				pm.SetPoolConfig("test", conf)
				sender := NewEnhancedSender("test-address", "test", pm, nil)
				envelope := &enhancedTestEnvelope{}
				var err error
				if reply {
					err = sender.SendResponse(nil, envelope)
				} else {
					err = sender.SendRequestAndRelease(nil, envelope)
				}
				if !errors.Is(err, sendErr) {
					t.Fatalf("error = %v, want %v", err, sendErr)
				}
				if envelope.releases != 1 {
					t.Fatalf("release count = %d, want 1", envelope.releases)
				}
				if reply && transport.responses != 1 || !reply && transport.requests != 1 {
					t.Fatal("wrong delivery method")
				}
				metrics, err := sender.GetMetrics()
				if err != nil {
					t.Fatal(err)
				}
				if metrics.TotalRequests != 1 {
					t.Fatalf("requests = %d, want 1", metrics.TotalRequests)
				}
			})
		}
	}
}
func testDeliveryName(reply bool, err error) string {
	name := "request"
	if reply {
		name = "response"
	}
	if err != nil {
		name += "-failure"
	}
	return name
}

func TestEnhancedSenderMissingPoolCreator(t *testing.T) {
	pm := pool.NewPoolManager(nil)
	defer pm.Close()
	sender := NewEnhancedSender("test-address", "missing", pm, nil)
	envelope := &enhancedTestEnvelope{}
	if err := sender.SendRequestAndRelease(nil, envelope); err == nil {
		t.Fatal("expected missing creator error")
	}
	if envelope.releases != 1 {
		t.Fatalf("release count = %d, want 1", envelope.releases)
	}
}

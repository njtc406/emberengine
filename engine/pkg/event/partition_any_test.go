package event

import (
	"context"
	"github.com/njtc406/emberengine/v2/engine/pkg/actor"
	"github.com/njtc406/emberengine/v2/engine/pkg/def"
	inf "github.com/njtc406/emberengine/v2/engine/pkg/interfaces"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"testing"
)

type partitionListener struct {
	inf.IListener
	pid      *actor.PID
	received []string
}

func (s *partitionListener) GetPid() *actor.PID   { return s.pid }
func (s *partitionListener) GetPartition() string { return s.pid.GetPartition() }
func (s *partitionListener) PostJob(j inf.IMailboxJob) error {
	defer j.Release()
	e := j.(interface{ GetPayload() *actor.Event }).GetPayload()
	s.received = append(s.received, e.GetPartition())
	return nil
}

type eventPartition int

func TestAnyPartitionPublishSubscribe(t *testing.T) {
	for _, publishLocal := range []bool{false, true} {
		for _, value := range []any{1, int64(1), eventPartition(1), "1", "001", true, 1.5, struct{ X int }{2}, nil, (*int)(nil)} {
			eb := NewEventBus()
			if err := eb.Init(nil, nil); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(eb.Stop)
			eb.serverPrefix = def.NatsDefaultServerPrefix
			normalized := actor.PartitionString(value)
			listener := &partitionListener{pid: actor.NewPID("", "node", normalized, "logic", "target", 1, "local")}
			other := &partitionListener{pid: actor.NewPID("", "node", "other", "logic", "other", 1, "local")}
			eb.SubscribeServer(1, listener)
			eb.SubscribeServer(1, other)
			var publish func(context.Context, def.EventType, any, proto.Message) error = eb.PublishServer
			if publishLocal {
				publish = eb.PublishServerLocal
			}
			if err := publish(context.Background(), 1, value, wrapperspb.String("payload")); err != nil {
				t.Fatal(err)
			}
			if len(listener.received) != 1 || listener.received[0] != normalized || len(other.received) != 0 {
				t.Fatalf("partition %T delivery mismatch", value)
			}
			eb.UnSubscribeServer(1, listener)
			if err := publish(context.Background(), 1, value, wrapperspb.String("payload")); err != nil {
				t.Fatal(err)
			}
			if len(listener.received) != 1 {
				t.Fatal("unsubscribed listener received event")
			}
		}
	}
}

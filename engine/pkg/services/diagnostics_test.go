package services

import (
	"github.com/njtc406/emberengine/v2/engine/pkg/def"
	inf "github.com/njtc406/emberengine/v2/engine/pkg/interfaces"
	"testing"
)

type metricsMailbox struct {
	inf.IMailbox
	metrics def.MailboxMetrics
}

func (m *metricsMailbox) GetMailboxMetrics() def.MailboxMetrics { return m.metrics }

type metricsService struct {
	inf.IService
	mb *metricsMailbox
}

func (s *metricsService) GetMailbox() inf.IMailbox { return s.mb }
func TestAggregatedMailboxMetrics(t *testing.T) {
	a := &metricsMailbox{metrics: def.MailboxMetrics{QueueBounded: true, QueueCapacity: 8, QueueLength: 3, ObservedQueueHighWatermark: 99, QueueFullRejectedTotal: 2}}
	b := &metricsMailbox{metrics: def.MailboxMetrics{QueueBounded: true, QueueCapacity: 16, QueueLength: 4, ObservedQueueHighWatermark: 88, ReadQueueFullDiscardedTotal: 5}}
	sm := &ServiceManager{runServices: []inf.IService{&metricsService{mb: a}, &metricsService{mb: b}}}
	m := sm.GetAggregatedMailboxMetrics()
	if !m.QueueBounded || m.QueueBackend != "bounded" || m.QueueCapacity != 24 || m.QueueLength != 7 || m.ObservedQueueHighWatermark != 7 || m.QueueFullRejectedTotal != 2 || m.ReadQueueFullDiscardedTotal != 5 {
		t.Fatalf("bounded: %+v", m)
	}
	b.metrics.QueueBounded = false
	b.metrics.QueueLength = 0
	m = sm.GetAggregatedMailboxMetrics()
	if m.QueueBackend != "mixed" || m.QueueBounded || m.QueueCapacity != 0 || m.ObservedQueueHighWatermark != 7 {
		t.Fatalf("mixed: %+v", m)
	}
	sm.runServices = sm.runServices[1:]
	if m = sm.GetAggregatedMailboxMetrics(); m.QueueBackend != "unbounded" {
		t.Fatalf("unbounded: %+v", m)
	}
	sm.runServices = nil
	if m = sm.GetAggregatedMailboxMetrics(); m.QueueBackend != "" || m.QueueBounded || m.QueueLength != 0 {
		t.Fatalf("empty: %+v", m)
	}
}

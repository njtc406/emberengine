package metrics

import (
	"testing"

	"github.com/njtc406/emberengine/v2/engine/pkg/def"
)

func TestMailboxMetricsQueueDescriptions(t *testing.T) {
	for _, backend := range []string{"", "bounded", "unbounded", "mixed"} {
		m := &def.MailboxMetrics{QueueBackend: backend, QueueBounded: backend == "bounded", QueueCapacity: 12, QueueLength: 3, QueueFullRejectedTotal: 2, ReadQueueFullDiscardedTotal: 4, ObservedQueueHighWatermark: 7}
		samples := MailboxMetricsToSamples(m)
		found := map[string]MetricSample{}
		for _, s := range samples {
			found[s.Desc.Name] = s
			if s.Desc.Name != "ember_mailbox_queue_backend_info" && len(s.Labels) != 0 {
				t.Fatal("high cardinality labels")
			}
		}
		for name, value := range map[string]float64{"ember_mailbox_queue_full_rejected_total": 2, "ember_mailbox_read_queue_full_discarded_total": 4, "ember_mailbox_queue_length": 3, "ember_mailbox_queue_observed_high_watermark": 7} {
			s, ok := found[name]
			if !ok || s.Value != value {
				t.Fatalf("missing/wrong %s: %+v", name, s)
			}
		}
		_, capacity := found["ember_mailbox_queue_capacity"]
		if capacity != (backend == "bounded") {
			t.Fatal("capacity presence", backend)
		}
		info, present := found["ember_mailbox_queue_backend_info"]
		if present != (backend != "") || (present && (info.Value != 1 || info.Labels["backend"] != backend)) {
			t.Fatal("backend info", backend)
		}
	}
}

func TestMailboxMetricsToSamples_Nil(t *testing.T) {
	if s := MailboxMetricsToSamples(nil); s != nil {
		t.Errorf("nil input should return nil, got %d samples", len(s))
	}
}

func TestMailboxMetricsToSamples_Count(t *testing.T) {
	m := &def.MailboxMetrics{PostTotal: 1}
	samples := MailboxMetricsToSamples(m)
	if len(samples) != 8 {
		t.Errorf("samples count = %d, want 8", len(samples))
	}
}

func TestMailboxMetricsToSamples_Values(t *testing.T) {
	m := &def.MailboxMetrics{
		PostTotal:           100,
		SuspendedTotal:      5,
		RejectedTotal:       3,
		DispatchFailedTotal: 2,
	}
	samples := MailboxMetricsToSamples(m)
	expected := map[string]float64{
		"ember_mailbox_post_total":            100,
		"ember_mailbox_suspended_total":       5,
		"ember_mailbox_rejected_total":        3,
		"ember_mailbox_dispatch_failed_total": 2,
	}
	for _, s := range samples {
		if want, ok := expected[s.Desc.Name]; ok {
			if s.Value != want {
				t.Errorf("%s = %v, want %v", s.Desc.Name, s.Value, want)
			}
		}
	}
}

func TestMailboxMetricsToSamples_AllCounters(t *testing.T) {
	m := &def.MailboxMetrics{}
	for _, s := range MailboxMetricsToSamples(m) {
		want := Counter
		if s.Desc.Name == "ember_mailbox_queue_length" || s.Desc.Name == "ember_mailbox_queue_observed_high_watermark" {
			want = Gauge
		}
		if s.Desc.Type != want {
			t.Errorf("%s type = %v, want %v", s.Desc.Name, s.Desc.Type, want)
		}
	}
}

func TestMailboxMetricsToText_Nil(t *testing.T) {
	text := MailboxMetricsToText(nil)
	if text != "" {
		t.Errorf("nil input should produce empty text, got %q", text)
	}
}

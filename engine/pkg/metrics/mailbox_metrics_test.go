package metrics

import (
	"testing"

	"github.com/njtc406/emberengine/engine/pkg/def"
)

func TestMailboxMetricsToSamples_Nil(t *testing.T) {
	if s := MailboxMetricsToSamples(nil); s != nil {
		t.Errorf("nil input should return nil, got %d samples", len(s))
	}
}

func TestMailboxMetricsToSamples_Count(t *testing.T) {
	m := &def.MailboxMetrics{PostTotal: 1}
	samples := MailboxMetricsToSamples(m)
	if len(samples) != 4 {
		t.Errorf("samples count = %d, want 4", len(samples))
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
		if s.Desc.Type != Counter {
			t.Errorf("%s type = %v, want Counter", s.Desc.Name, s.Desc.Type)
		}
	}
}

func TestMailboxMetricsToText_Nil(t *testing.T) {
	text := MailboxMetricsToText(nil)
	if text != "" {
		t.Errorf("nil input should produce empty text, got %q", text)
	}
}

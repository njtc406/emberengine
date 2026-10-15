package metrics

import (
	"testing"

	"github.com/njtc406/emberengine/engine/pkg/event"
)

func TestEventMetricsToSamples_Nil(t *testing.T) {
	if s := EventMetricsToSamples(nil); s != nil {
		t.Errorf("nil input should return nil, got %d samples", len(s))
	}
}

func TestEventMetricsToSamples_Count(t *testing.T) {
	m := &event.EventMetrics{TotalPublished: 1}
	samples := EventMetricsToSamples(m)
	if len(samples) != 4 {
		t.Errorf("samples count = %d, want 4", len(samples))
	}
}

func TestEventMetricsToSamples_Values(t *testing.T) {
	m := &event.EventMetrics{
		TotalPublished: 100,
		TotalDelivered: 90,
		TotalThrottled: 5,
		TotalBatched:   80,
	}
	samples := EventMetricsToSamples(m)
	expected := map[string]float64{
		"ember_event_published_total": 100,
		"ember_event_delivered_total": 90,
		"ember_event_throttled_total": 5,
		"ember_event_batched_total":   80,
	}
	for _, s := range samples {
		if want, ok := expected[s.Desc.Name]; ok {
			if s.Value != want {
				t.Errorf("%s = %v, want %v", s.Desc.Name, s.Value, want)
			}
		}
	}
}

func TestEventMetricsToSamples_AllCounters(t *testing.T) {
	m := &event.EventMetrics{}
	for _, s := range EventMetricsToSamples(m) {
		if s.Desc.Type != Counter {
			t.Errorf("%s type = %v, want Counter", s.Desc.Name, s.Desc.Type)
		}
	}
}

func TestEventMetricsToText_Nil(t *testing.T) {
	text := EventMetricsToText(nil)
	if text != "" {
		t.Errorf("nil input should produce empty text, got %q", text)
	}
}

package metrics

import (
	"strings"
	"testing"

	"github.com/njtc406/emberengine/engine/pkg/rpc/message/msgbus"
)

func TestRpcMetricsToSamples_Nil(t *testing.T) {
	samples := RpcMetricsToSamples(nil)
	if len(samples) != 0 {
		t.Errorf("nil RpcMetrics should produce 0 samples, got %d", len(samples))
	}
}

func TestRpcMetricsToSamples_Count(t *testing.T) {
	m := &msgbus.RpcMetrics{}
	samples := RpcMetricsToSamples(m)
	// 7 指标: call_total, call_errors, call_in_flight, async_call_total, async_call_errors, send_total, send_errors
	if len(samples) != 7 {
		t.Errorf("RpcMetrics samples count = %d, want 7", len(samples))
	}
}

func TestRpcMetricsToText_Values(t *testing.T) {
	m := &msgbus.RpcMetrics{
		CallTotal:       100,
		CallErrors:      5,
		CallInFlight:    3,
		AsyncCallTotal:  200,
		AsyncCallErrors: 10,
		SendTotal:       500,
		SendErrors:      2,
	}
	text := RpcMetricsToText(m)

	checks := []struct {
		name  string
		value string
	}{
		{"ember_rpc_call_total", "ember_rpc_call_total 100"},
		{"ember_rpc_call_errors_total", "ember_rpc_call_errors_total 5"},
		{"ember_rpc_call_in_flight", "ember_rpc_call_in_flight 3"},
		{"ember_rpc_async_call_total", "ember_rpc_async_call_total 200"},
		{"ember_rpc_async_call_errors_total", "ember_rpc_async_call_errors_total 10"},
		{"ember_rpc_send_total", "ember_rpc_send_total 500"},
		{"ember_rpc_send_errors_total", "ember_rpc_send_errors_total 2"},
	}

	for _, c := range checks {
		if !strings.Contains(text, c.value) {
			t.Errorf("missing %s in:\n%s", c.name, text)
		}
	}
}

func TestRpcMetricsToText_Types(t *testing.T) {
	m := &msgbus.RpcMetrics{}
	text := RpcMetricsToText(m)

	// Counter types
	for _, name := range []string{
		"ember_rpc_call_total",
		"ember_rpc_call_errors_total",
		"ember_rpc_async_call_total",
		"ember_rpc_async_call_errors_total",
		"ember_rpc_send_total",
		"ember_rpc_send_errors_total",
	} {
		expected := "# TYPE " + name + " counter"
		if !strings.Contains(text, expected) {
			t.Errorf("missing TYPE counter for %s in:\n%s", name, text)
		}
	}

	// Gauge types
	expected := "# TYPE ember_rpc_call_in_flight gauge"
	if !strings.Contains(text, expected) {
		t.Errorf("missing TYPE gauge for call_in_flight in:\n%s", text)
	}
}

func TestRpcMetricsToText_Zero(t *testing.T) {
	m := &msgbus.RpcMetrics{}
	text := RpcMetricsToText(m)
	if !strings.Contains(text, "ember_rpc_call_total 0") {
		t.Errorf("zero metrics should output 0:\n%s", text)
	}
}

func TestRpcMetricsToText_Nil(t *testing.T) {
	text := RpcMetricsToText(nil)
	if text != "" {
		t.Errorf("nil should produce empty text, got: %q", text)
	}
}

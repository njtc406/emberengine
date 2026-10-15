package metrics

import (
	"strings"
	"testing"
)

// --- SamplesToText ---

func TestSamplesToText_Empty(t *testing.T) {
	if text := SamplesToText(nil); text != "" {
		t.Errorf("nil samples should produce empty text, got: %q", text)
	}
	if text := SamplesToText([]MetricSample{}); text != "" {
		t.Errorf("empty samples should produce empty text, got: %q", text)
	}
}

func TestSamplesToText_SingleSample(t *testing.T) {
	samples := []MetricSample{
		{
			Desc:   MetricDesc{Name: "test_gauge", Help: "A test gauge", Type: Gauge},
			Labels: map[string]string{"env": "prod"},
			Value:  42,
		},
	}
	text := SamplesToText(samples)
	if !strings.Contains(text, "# HELP test_gauge A test gauge") {
		t.Errorf("missing HELP line in:\n%s", text)
	}
	if !strings.Contains(text, "# TYPE test_gauge gauge") {
		t.Errorf("missing TYPE line in:\n%s", text)
	}
	if !strings.Contains(text, `test_gauge{env="prod"} 42`) {
		t.Errorf("missing sample line in:\n%s", text)
	}
}

func TestSamplesToText_CounterType(t *testing.T) {
	samples := []MetricSample{
		{
			Desc:  MetricDesc{Name: "test_counter", Help: "A counter", Type: Counter},
			Value: 100,
		},
	}
	text := SamplesToText(samples)
	if !strings.Contains(text, "# TYPE test_counter counter") {
		t.Errorf("wrong TYPE for counter in:\n%s", text)
	}
}

func TestSamplesToText_NoLabels(t *testing.T) {
	samples := []MetricSample{
		{Desc: MetricDesc{Name: "bare_metric", Help: "no labels", Type: Gauge}, Value: 1},
	}
	text := SamplesToText(samples)
	if !strings.Contains(text, "bare_metric 1") {
		t.Errorf("bare metric should have no label braces, got:\n%s", text)
	}
	if strings.Contains(text, "{") {
		t.Errorf("should not contain braces for empty labels, got:\n%s", text)
	}
}

func TestSamplesToText_SameNameGrouped(t *testing.T) {
	desc := MetricDesc{Name: "grouped", Help: "test", Type: Gauge}
	samples := []MetricSample{
		{Desc: desc, Labels: map[string]string{"a": "1"}, Value: 10},
		{Desc: desc, Labels: map[string]string{"a": "2"}, Value: 20},
	}
	text := SamplesToText(samples)
	// HELP/TYPE 只出现一次
	if strings.Count(text, "# HELP grouped") != 1 {
		t.Errorf("HELP should appear once, got:\n%s", text)
	}
	if strings.Count(text, "# TYPE grouped") != 1 {
		t.Errorf("TYPE should appear once, got:\n%s", text)
	}
}

// --- formatLabels 排序 ---

func TestFormatLabels_Sorted(t *testing.T) {
	labels := map[string]string{
		"z_label": "last",
		"a_label": "first",
		"m_label": "middle",
	}
	result := formatLabels(labels)
	expected := `{a_label="first",m_label="middle",z_label="last"}`
	if result != expected {
		t.Errorf("formatLabels = %q, want %q", result, expected)
	}
}

func TestFormatLabels_Empty(t *testing.T) {
	if result := formatLabels(nil); result != "" {
		t.Errorf("nil labels should be empty, got %q", result)
	}
	if result := formatLabels(map[string]string{}); result != "" {
		t.Errorf("empty labels should be empty, got %q", result)
	}
}

// --- escapeLabelValue ---

func TestEscapeLabelValue_All(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"simple", "simple"},
		{`has"quote`, `has\"quote`},
		{`has\back`, `has\\back`},
		{"has\nnewline", `has\nnewline`},
		{`"a\nb"`, `\"a\\nb\"`},
	}
	for _, c := range cases {
		got := escapeLabelValue(c.in)
		if got != c.want {
			t.Errorf("escapeLabelValue(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

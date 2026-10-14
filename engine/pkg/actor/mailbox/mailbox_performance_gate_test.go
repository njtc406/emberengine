package mailbox

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"testing"
	"time"
)

func perfMedian(values []float64) float64 {
	sort.Float64s(values)
	return values[len(values)/2]
}
func compareMailboxPerf(base, current []mailboxPerfRow) []string {
	var failures []string
	for _, scenario := range mailboxPerfScenarios() {
		for _, candidate := range []string{"unbounded", "bounded"} {
			selectRows := func(rows []mailboxPerfRow, name string) []mailboxPerfRow {
				var out []mailboxPerfRow
				for _, r := range rows {
					if r.Scenario == scenario.Name && r.Candidate == name {
						out = append(out, r)
					}
				}
				return out
			}
			b, c := selectRows(base, "baseline"), selectRows(current, candidate)
			label := scenario.Name + "/" + candidate
			if len(b) != 5 || len(c) != 5 {
				failures = append(failures, label+": need exactly five rounds")
				continue
			}
			valid := true
			for _, group := range [][]mailboxPerfRow{b, c} {
				rounds := map[int]bool{}
				for _, r := range group {
					m, bm := r.Machine, b[0].Machine
					if rounds[r.Round] || r.Protocol != "task2-v2" || m.CPU == "" || m.CPU != bm.CPU || m.OS != bm.OS || m.Arch != bm.Arch || m.Go != bm.Go || m.Procs != bm.Procs || m.Commit == "" || r.Samples != b[0].Samples || r.Workers != scenario.Workers || r.EffectiveRW != scenario.RW || !r.Drained || r.Success+r.MainRejected+r.RWDropped+r.StopRejected+r.OtherRejected != r.Samples || r.CPU <= 0 || r.Wall <= 0 || r.Success <= 0 {
						valid = false
					}
					rounds[r.Round] = true
					if scenario.Kind != "overload" && (r.Success != r.Samples || r.MainRejected != 0 || r.RWDropped != 0 || r.OtherRejected != 0 || r.StopRejected != 0) {
						valid = false
					}
				}
			}
			if !valid {
				failures = append(failures, label+": invalid metadata, samples, conservation, CPU resolution or loss")
				continue
			}
			if scenario.Kind == "overload" {
				continue
			}
			metrics := []struct {
				name     string
				min, max float64
				value    func(mailboxPerfRow) float64
			}{
				{"throughput", .95, math.Inf(1), func(r mailboxPerfRow) float64 { return float64(r.Success) / r.Wall.Seconds() }},
				{"p99", 0, 1.10, func(r mailboxPerfRow) float64 { return float64(r.Completion.P99) }},
				{"p999", 0, 1.15, func(r mailboxPerfRow) float64 { return float64(r.Completion.P999) }},
				{"CPU/success", 0, 1.10, func(r mailboxPerfRow) float64 { return float64(r.CPU) / float64(r.Success) }},
			}
			if scenario.Kind == "mostly-empty" {
				metrics = append(metrics, struct {
					name     string
					min, max float64
					value    func(mailboxPerfRow) float64
				}{"idle-CPU", 0, 1.10, func(r mailboxPerfRow) float64 { return float64(r.CPU) }})
			}
			for _, metric := range metrics {
				values := func(rows []mailboxPerfRow) []float64 {
					out := make([]float64, len(rows))
					for i, r := range rows {
						out[i] = metric.value(r)
					}
					return out
				}
				denominator := perfMedian(values(b))
				if denominator <= 0 {
					failures = append(failures, label+"/"+metric.name+": unresolved baseline")
					continue
				}
				ratio := perfMedian(values(c)) / denominator
				if math.IsNaN(ratio) || ratio < metric.min || ratio > metric.max {
					failures = append(failures, fmt.Sprintf("%s/%s ratio %.4f outside [%.2f,%.2f]", label, metric.name, ratio, metric.min, metric.max))
				}
			}
		}
	}
	return failures
}
func TestMailboxPerformanceComparison(t *testing.T) {
	var base, current []mailboxPerfRow
	for _, s := range mailboxPerfScenarios() {
		for round := 0; round < 5; round++ {
			r := mailboxPerfRow{Protocol: "task2-v2", Scenario: s.Name, Round: round, Workers: s.Workers, EffectiveRW: s.RW, Samples: 100, Success: 100, Wall: time.Second, CPU: time.Second, Drained: true}
			r.Machine.CPU = "test-cpu"
			r.Machine.OS = "test-os"
			r.Machine.Commit = "73ecd28"
			r.Completion.P99 = time.Millisecond
			r.Completion.P999 = time.Millisecond
			r.Candidate = "baseline"
			base = append(base, r)
			for _, name := range []string{"unbounded", "bounded"} {
				r.Candidate = name
				current = append(current, r)
			}
		}
	}
	if got := compareMailboxPerf(base, current); len(got) > 0 {
		t.Fatal(got)
	}
	for _, tc := range []struct {
		name   string
		mutate func([]mailboxPerfRow)
	}{
		{"missing", func(r []mailboxPerfRow) { r[0].Scenario = "missing" }},
		{"metadata", func(r []mailboxPerfRow) { r[0].Machine.CPU = "other" }},
		{"cpu-zero", func(r []mailboxPerfRow) { r[0].CPU = 0 }},
		{"loss", func(r []mailboxPerfRow) { r[0].Success-- }},
		{"duplicate-round", func(r []mailboxPerfRow) { r[0].Round = 1 }},
		{"p99", func(r []mailboxPerfRow) {
			for i := range r {
				r[i].Completion.P99 *= 2
			}
		}},
		{"cpu", func(r []mailboxPerfRow) {
			for i := range r {
				r[i].CPU *= 2
			}
		}},
		{"throughput", func(r []mailboxPerfRow) {
			for i := range r {
				r[i].Wall *= 2
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copyRows := append([]mailboxPerfRow(nil), current...)
			tc.mutate(copyRows)
			if got := compareMailboxPerf(base, copyRows); len(got) == 0 {
				t.Fatal("bad samples accepted")
			}
		})
	}
	if got := compareMailboxPerf(nil, nil); len(got) == 0 || !strings.Contains(got[0], "five rounds") {
		t.Fatal(got)
	}
}

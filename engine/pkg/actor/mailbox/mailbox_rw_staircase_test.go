package mailbox

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/njtc406/emberengine/v2/engine/internal/perftest"
	"github.com/njtc406/emberengine/v2/engine/pkg/def"
	"math/rand"
	"os"
	"reflect"
	"runtime"
	"runtime/trace"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRWDiagnosticSequence(t *testing.T) {
	for _, order := range []string{"rrrw", "batch", "random"} {
		a := rwDiagnosticSequence(128, order)
		if len(a) != 128 {
			t.Fatalf("%s length=%d", order, len(a))
		}
		reads := 0
		for _, r := range a {
			if r {
				reads++
			}
		}
		if reads != 96 {
			t.Fatalf("%s reads=%d", order, reads)
		}
		if !reflect.DeepEqual(a, rwDiagnosticSequence(128, order)) {
			t.Fatal("not reproducible")
		}
		if order == "rrrw" {
			for i, r := range a {
				if r != (i%4 != 3) {
					t.Fatal("RRRW layout")
				}
			}
		}
		if order == "batch" {
			for i, r := range a {
				if r != (i%128 < 96) {
					t.Fatal("batch layout")
				}
			}
		}
	}
	if reflect.DeepEqual(rwDiagnosticSequence(128, "random"), rwDiagnosticSequence(128, "rrrw")) {
		t.Fatal("random did not permute")
	}
}

func rwDiagnosticSequence(n int, order string) []bool {
	a := make([]bool, n)
	for i := range a {
		a[i] = i%4 != 3
		if order == "batch" {
			block := 128
			if remaining := n - (i/128)*128; remaining < block {
				block = remaining
			}
			a[i] = i%128 < block*3/4
		}
	}
	if order == "random" {
		rand.New(rand.NewSource(20261029)).Shuffle(n, func(i, j int) { a[i], a[j] = a[j], a[i] })
	}
	return a
}

// TestMailboxRWStaircase is an opt-in diagnostic, not an acceptance protocol.
func TestMailboxRWStaircase(t *testing.T) {
	if os.Getenv("MAILBOX_RW_STAIRCASE") != "1" {
		t.Skip("explicit diagnostic only")
	}
	if runtime.GOMAXPROCS(0) != 4 {
		t.Fatal("diagnostic requires GOMAXPROCS=4")
	}
	rates := []int{4000, 8000, 12000, 16000, 24000, 32000}
	if text := os.Getenv("RW_RATES"); text != "" {
		rates = nil
		for _, text := range strings.Split(text, ",") {
			r, err := strconv.Atoi(text)
			if err != nil || r < 1000 || r > 32000 || r%1000 != 0 {
				t.Fatal("rates must be 1k multiples within 1..32k")
			}
			rates = append(rates, r)
		}
	}
	orders := []string{"rrrw"}
	if os.Getenv("RW_ORDERS") != "" {
		orders = strings.Split(os.Getenv("RW_ORDERS"), ",")
	}
	backends := []string{"bounded", "unbounded"}
	if os.Getenv("RW_REVERSE") == "1" {
		backends = []string{"unbounded", "bounded"}
	}
	for _, order := range orders {
		if order != "rrrw" && order != "batch" && order != "random" {
			t.Fatal("invalid order")
		}
		for _, rate := range rates {
			for _, backend := range backends {
				name := fmt.Sprintf("%s-%d-%s", order, rate, backend)
				t.Run(name, func(t *testing.T) {
					region := trace.StartRegion(context.Background(), name)
					defer region.End()
					conf := newSimpleConf(4)
					conf.EnableRWMode = true
					conf.MaxConcurrentReads = 32
					if err := json.Unmarshal([]byte(fmt.Sprintf(`{"QueueBackend":%q,"DualQueueCapacity":{"System":4096,"User":4096},"MaxQueueStorageBytes":16777216}`, backend)), conf); err != nil {
						t.Fatal(err)
					}
					inv := &perfInvoker{work: 64}
					mb, err := NewMailbox(conf, &perfLogger{}, inv, nil)
					if err != nil {
						t.Fatal(err)
					}
					mb.Start()
					defer mb.Stop()
					// Exactly two seconds of nominal input, capped at 64000 jobs even for unbounded.
					n := rate * 2
					sequence := rwDiagnosticSequence(n, order)
					jobs := make([]*perfJob, n)
					var released atomic.Int64
					reads, switches := 0, 0
					for i, r := range sequence {
						j := &perfJob{IMailboxJob: newMetricsTestJob("hot"), released: &released}
						if r {
							j.SetRWMode(def.RWModeRead)
							reads++
						}
						if i > 0 && r != sequence[i-1] {
							switches++
						}
						jobs[i] = j
					}
					// Truncated batch tails are balanced without changing task count.
					if reads != n*3/4 {
						t.Fatalf("unbalanced workload: %d/%d", reads, n)
					}
					type point struct {
						NS                                  int64
						Offered, Rejected, Queue, ReadQueue int
						Released                            int64
						Dispatched, Launched                uint64
						WriteWaitNS                         int64
						WriteWaitCount                      int64
					}
					points := make([]point, 0, 202)
					rejected := 0
					cpu0, err := perftest.CPUTime()
					if err != nil {
						t.Fatal(err)
					}
					start := time.Now()
					sample := func(offered int) {
						p := point{NS: time.Since(start).Nanoseconds(), Offered: offered, Rejected: rejected, Released: released.Load()}
						for _, w := range mb.workerPool.snap.Load().workers {
							w := w.(*Worker)
							p.Queue += w.GetJobLen()
							p.ReadQueue += len(w.readCh)
							p.Dispatched += w.readsDispatched.Load()
							p.Launched += w.readsLaunched.Load()
							p.WriteWaitNS += w.rwWriteWaitSum.Load()
							p.WriteWaitCount += w.rwWriteWaitCount.Load()
						}
						points = append(points, p)
					}
					batch := rate / 1000
					maxLate := time.Duration(0)
					for i, j := range jobs {
						if i%batch == 0 {
							due := start.Add(time.Duration(i/batch) * time.Millisecond)
							if d := time.Until(due); d > 0 {
								time.Sleep(d)
							}
							late := time.Since(due)
							if late > maxLate {
								maxLate = late
							}
							if time.Since(start) > 5*time.Second {
								t.Fatal("offer exceeded 5s bound")
							}
							if (i/batch)%10 == 0 {
								sample(i)
							}
						}
						j.started = perftest.Now()
						if err := mb.PostJob(j); err != nil {
							if err != def.ErrMailboxWorkerIsFull {
								t.Fatal(err)
							}
							rejected++
						}
					}
					offered := time.Since(start)
					sample(n)
					endReleased := released.Load()
					deadline := time.Now().Add(10 * time.Second)
					for released.Load() != int64(n) && time.Now().Before(deadline) {
						time.Sleep(time.Millisecond)
					}
					wall := time.Since(start)
					cpu1, err := perftest.CPUTime()
					if err != nil {
						t.Fatal(err)
					}
					sample(n)
					if released.Load() != int64(n) {
						t.Fatalf("drain %d/%d", released.Load(), n)
					}
					lat := make([]time.Duration, 0, n)
					for _, j := range jobs {
						if j.executed.Load() {
							lat = append(lat, j.latency)
						}
					}
					dropped := int(inv.discarded.Load()) - rejected
					if len(lat)+rejected+dropped != n || dropped != 0 {
						t.Fatalf("conservation success=%d rejected=%d dropped=%d", len(lat), rejected, dropped)
					}
					result := struct {
						Name                                                     string
						Rate, Tasks, Reads, Switches, Success, Rejected, Dropped int
						Offered, Wall, Drain, CPU, MaxLate                       time.Duration
						CPUResolved                                              bool
						CompletedAtOfferEnd                                      int64
						Completion                                               perftest.Quantiles
						Points                                                   []point
					}{name, rate, n, reads, switches, len(lat), rejected, dropped, offered, wall, wall - offered, cpu1 - cpu0, maxLate, mailboxPerfCPUResolved(cpu1 - cpu0), endReleased - int64(rejected), perftest.Percentiles(lat), points}
					data, err := json.MarshalIndent(result, "", "  ")
					if err != nil {
						t.Fatal(err)
					}
					if prefix := os.Getenv("RW_STAIRCASE_OUTPUT"); prefix != "" {
						if err := os.WriteFile(prefix+"-"+name+".json", data, 0644); err != nil {
							t.Fatal(err)
						}
					}
					t.Logf("%s tasks=%d success=%d rejected=%d endCompleted=%d drain=%v p99=%v CPU=%v resolved=%v", name, n, len(lat), rejected, result.CompletedAtOfferEnd, result.Drain, result.Completion.P99, result.CPU, result.CPUResolved)
				})
			}
		}
	}
}

package mailbox

import (
	"math"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/njtc406/emberengine/v2/engine/pkg/config"
	"github.com/njtc406/emberengine/v2/engine/pkg/def"
	inf "github.com/njtc406/emberengine/v2/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/v2/engine/pkg/utils/boundedmpsc"
)

func boundedQueueConfig() *config.MailboxConf {
	return &config.MailboxConf{QueueBackend: "bounded", QueueMode: "dual", DualQueueCapacity: &config.DualQueueCapacityConf{System: 2, User: 4}, MaxQueueStorageBytes: 1 << 20, SchedulePolicy: &config.WorkerSchedulePolicy{InitialWorkerNum: 2}}
}

func priorityQueueConfig() *config.MailboxConf {
	c := boundedQueueConfig()
	c.QueueMode = "priority"
	c.SchedulePolicy.MultiLevelQueueConf = &config.MultiLevelQueueConf{Strategy: def.StrategyFairness, TotalBatchLimit: 17, PriorityBatches: map[def.Priority]*config.PriorityConfig{
		def.PrioritySys: {Capacity: 2, BatchSize: 3, Weight: 4}, def.PriorityUrgent: {Capacity: 4}, def.PriorityHigh: {Capacity: 8}, def.PriorityNormal: {Capacity: 16}, def.PriorityLow: {Capacity: 32},
	}}
	return c
}

func TestBoundedQueueConfig(t *testing.T) {
	cases := []struct {
		name   string
		change func(*config.MailboxConf)
		bad    bool
	}{
		{"valid dual", func(c *config.MailboxConf) {}, false},
		{"unknown backend", func(c *config.MailboxConf) { c.QueueBackend = "sequence" }, true},
		{"unknown mode", func(c *config.MailboxConf) { c.QueueMode = "other" }, true},
		{"default mode", func(c *config.MailboxConf) { c.QueueMode = "" }, false},
		{"missing capacity", func(c *config.MailboxConf) { c.DualQueueCapacity = nil }, false},
		{"zero", func(c *config.MailboxConf) { c.DualQueueCapacity.System = 0 }, false},
		{"one", func(c *config.MailboxConf) { c.DualQueueCapacity.User = 1 }, true},
		{"three", func(c *config.MailboxConf) { c.DualQueueCapacity.User = 3 }, true},
		{"negative", func(c *config.MailboxConf) { c.DualQueueCapacity.System = -2 }, true},
		{"missing budget", func(c *config.MailboxConf) { c.MaxQueueStorageBytes = 0 }, false},
		{"small budget", func(c *config.MailboxConf) { c.MaxQueueStorageBytes = 1 }, true},
		{"array bytes overflow", func(c *config.MailboxConf) {
			c.DualQueueCapacity.User = int(^uint(0)>>2) + 1
			c.MaxQueueStorageBytes = math.MaxUint64
		}, true},
		{"no scaling strategy", func(c *config.MailboxConf) { c.SchedulePolicy.EnableAutoScaling = true }, true},
		{"max below initial", func(c *config.MailboxConf) {
			c.SchedulePolicy.EnableAutoScaling = true
			c.SchedulePolicy.ScalingStrategy = &config.WorkerStrategyConfig{MaxWorkerNum: 1, ResizeCoolDown: time.Second}
		}, true},
		{"zero cooldown", func(c *config.MailboxConf) {
			c.SchedulePolicy.EnableAutoScaling = true
			c.SchedulePolicy.ScalingStrategy = &config.WorkerStrategyConfig{MaxWorkerNum: 4}
		}, true},
		{"negative cooldown", func(c *config.MailboxConf) {
			c.SchedulePolicy.EnableAutoScaling = true
			c.SchedulePolicy.ScalingStrategy = &config.WorkerStrategyConfig{MaxWorkerNum: 4, ResizeCoolDown: -1}
		}, true},
		{"min repairs max", func(c *config.MailboxConf) {
			c.SchedulePolicy.EnableAutoScaling = true
			c.SchedulePolicy.ScalingStrategy = &config.WorkerStrategyConfig{MinWorkerNum: 3, MaxWorkerNum: 1, ResizeCoolDown: time.Second}
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := boundedQueueConfig()
			tc.change(c)
			p, err := NewWorkerPool(c, nil, &mockInvoker{})
			if (err != nil) != tc.bad {
				t.Fatalf("pool=%v error=%v, want error=%v", p, err, tc.bad)
			}
			if p != nil {
				p.cancel()
				if p.snap.Load() != nil {
					t.Fatal("constructor created workers")
				}
			}
		})
	}
}

func TestPriorityQueueConfig(t *testing.T) {
	for _, name := range []string{"valid", "nil conf", "empty", "nil entry", "missing sys", "missing urgent", "missing high", "no user", "bad capacity"} {
		t.Run(name, func(t *testing.T) {
			c := priorityQueueConfig()
			q := c.SchedulePolicy.MultiLevelQueueConf
			switch name {
			case "nil conf":
				c.SchedulePolicy.MultiLevelQueueConf = nil
			case "empty":
				q.PriorityBatches = nil
			case "nil entry":
				q.PriorityBatches[def.PrioritySys] = nil
			case "missing sys":
				delete(q.PriorityBatches, def.PrioritySys)
			case "missing urgent":
				delete(q.PriorityBatches, def.PriorityUrgent)
			case "missing high":
				delete(q.PriorityBatches, def.PriorityHigh)
			case "no user":
				delete(q.PriorityBatches, def.PriorityNormal)
				delete(q.PriorityBatches, def.PriorityLow)
			case "bad capacity":
				q.PriorityBatches[def.PriorityNormal].Capacity = 3
			}
			s, err := normalizeQueueSpec(fixConf(c))
			if (err != nil) != (name != "valid" && name != "nil conf" && name != "empty" && name != "nil entry") {
				t.Fatalf("spec=%+v error=%v", s, err)
			}
			if name == "valid" && (s.priority.fallbackPriority != def.PriorityLow || s.priority.strategy != def.StrategyFairness || s.priority.totalBatchLimit != 17) {
				t.Fatalf("incomplete spec: %+v", s.priority)
			}
		})
	}
}

func TestUnboundedQueueConfig(t *testing.T) {
	for _, backend := range []string{"unbounded"} {
		for _, mode := range []string{"", "dual", "priority", "unknown"} {
			t.Run(backend+"/"+mode, func(t *testing.T) {
				c := &config.MailboxConf{QueueBackend: backend, QueueMode: mode, SchedulePolicy: &config.WorkerSchedulePolicy{EnableAutoScaling: true}}
				p, err := NewWorkerPool(c, nil, &mockInvoker{})
				if err != nil {
					t.Fatal(err)
				}
				defer p.cancel()
				if p.queueSpec.backend != "unbounded" || p.queueSpec.storageBytes != 0 {
					t.Fatalf("%+v", p.queueSpec)
				}
				c.SchedulePolicy.ScalingStrategy = &config.WorkerStrategyConfig{ResizeCoolDown: -1}
				p2, err := NewWorkerPool(c, nil, &mockInvoker{})
				if err != nil {
					t.Fatal(err)
				}
				p2.cancel()
			})
		}
	}
}

func TestQueueConfigBudget(t *testing.T) {
	fixed := uint64(unsafe.Sizeof(boundedmpsc.Queue[inf.IMailboxJob]{}))
	slot := uint64(unsafe.Sizeof(*new(inf.IMailboxJob)))
	for _, auto := range []bool{false, true} {
		c := boundedQueueConfig()
		c.SchedulePolicy.EnableAutoScaling = auto
		c.SchedulePolicy.ScalingStrategy = &config.WorkerStrategyConfig{MinWorkerNum: 5, MaxWorkerNum: 3, ResizeCoolDown: time.Second}
		workers := uint64(2)
		if auto {
			workers = 5
		}
		want := workers * (2*fixed + 6*slot)
		c.MaxQueueStorageBytes = want
		s, err := normalizeQueueSpec(fixConf(c))
		if err != nil || s.storageBytes != want {
			t.Fatalf("auto=%v bytes=%d want=%d err=%v", auto, s.storageBytes, want, err)
		}
		c.MaxQueueStorageBytes--
		if _, err := normalizeQueueSpec(fixConf(c)); err == nil {
			t.Fatal("accepted budget overrun")
		}
	}
	if _, err := checkedQueueAdd(math.MaxUint64, 1); err == nil {
		t.Fatal("addition overflow")
	}
	if _, err := checkedQueueMul(math.MaxUint64, 2); err == nil {
		t.Fatal("multiplication overflow")
	}
	c := priorityQueueConfig()
	s, err := normalizeQueueSpec(fixConf(c))
	want := uint64(2) * (5*fixed + 62*slot)
	if err != nil || s.storageBytes != want {
		t.Fatalf("priority bytes=%d want=%d err=%v", s.storageBytes, want, err)
	}
}

func TestQueueConfigCopies(t *testing.T) {
	c := priorityQueueConfig()
	before := priorityQueueConfig()
	p, err := NewWorkerPool(c, nil, &mockInvoker{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.cancel()
	if !reflect.DeepEqual(c, before) {
		t.Fatal("constructor mutated caller")
	}
	q := c.SchedulePolicy.MultiLevelQueueConf
	cloned := clonePriorityBatches(q.PriorityBatches)
	if cloned[def.PrioritySys].Capacity != 2 {
		t.Fatal("clone lost capacity")
	}
	q.PriorityBatches[def.PrioritySys].Capacity = 64
	q.Strategy = def.StrategyAbsolute
	c.DualQueueCapacity.System = 64
	if p.queueSpec.priority.priorities[def.PrioritySys].Capacity != 2 || p.queueSpec.priority.strategy != def.StrategyFairness {
		t.Fatal("spec aliases caller")
	}
	c = boundedQueueConfig()
	s, err := normalizeQueueSpec(fixConf(c))
	if err != nil {
		t.Fatal(err)
	}
	c.DualQueueCapacity.User = 128
	if s.dual.user != 4 {
		t.Fatal("dual spec aliases caller")
	}
	c = boundedQueueConfig()
	c.DualQueueCapacity.User = 3
	before = boundedQueueConfig()
	before.DualQueueCapacity.User = 3
	if _, err := NewWorkerPool(c, nil, &mockInvoker{}); err == nil {
		t.Fatal("wanted error")
	}
	if !reflect.DeepEqual(c, before) {
		t.Fatal("failed constructor mutated caller")
	}
}

func TestQueueConfigWorkerIsolation(t *testing.T) {
	c := priorityQueueConfig()
	c.QueueBackend = "unbounded"
	p, err := NewWorkerPool(c, nil, &mockInvoker{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.cancel()
	// Initial and resized workers consume the same frozen spec, not this mutable input.
	c.SchedulePolicy.MultiLevelQueueConf.PriorityBatches[def.PrioritySys].Weight = 999
	p.conf.SchedulePolicy.MultiLevelQueueConf = nil
	a := newWorker(0, p.conf, p.queueSpec, p.workerEnv(), DrainExecute).(*Worker)
	b := newWorker(1, p.conf, p.queueSpec, p.workerEnv(), DrainExecute).(*Worker)
	qa := a.queueManager.(*PriorityQueueManager)
	qb := b.queueManager.(*PriorityQueueManager)
	if qa.scheduler.strategy != def.StrategyFairness || qa.totalBatchLimit != 17 || qa.scheduler.priorities[def.PrioritySys].Capacity != 2 || qa.scheduler.weights[def.PrioritySys] != 4 {
		t.Fatal("lost normalized scheduling settings")
	}
	qa.scheduler.counters[def.PrioritySys] = 11
	qa.scheduler.priorities[def.PrioritySys].Capacity = 128
	if qb.scheduler.counters[def.PrioritySys] != 0 || qb.scheduler.priorities[def.PrioritySys].Capacity != 2 || p.queueSpec.priority.priorities[def.PrioritySys].Capacity != 2 {
		t.Fatal("workers share mutable scheduling state")
	}
}

func TestQueueConfigBudgetOverflow(t *testing.T) {
	if unsafe.Sizeof(int(0)) < 8 {
		t.Skip("64-bit layout overflow cases")
	}
	c := boundedQueueConfig()
	c.MaxQueueStorageBytes = math.MaxUint64
	// Each array fits platform int; two queues times workers exceed uint64.
	c.DualQueueCapacity.System = int(uint64(1) << 58)
	c.DualQueueCapacity.User = int(uint64(1) << 58)
	if _, err := normalizeQueueSpec(fixConf(c)); err == nil {
		t.Fatal("accepted worker multiplication overflow")
	}
	c = priorityQueueConfig()
	c.MaxQueueStorageBytes = math.MaxUint64
	for _, pc := range c.SchedulePolicy.MultiLevelQueueConf.PriorityBatches {
		pc.Capacity = int(uint64(1) << 58)
	}
	if _, err := normalizeQueueSpec(fixConf(c)); err == nil {
		t.Fatal("accepted physical layout sum overflow")
	}
}

func TestQueueConfigBoundedNotSilentlyUnbounded(t *testing.T) {
	s, err := normalizeQueueSpec(fixConf(boundedQueueConfig()))
	if err != nil {
		t.Fatal(err)
	}
	m := createQueueManager(s, nil).(*DualQueueManager)
	for _, q := range []queue[inf.IMailboxJob]{m.systemMailbox, m.userMailbox} {
		if _, ok := q.(*boundedmpsc.Queue[inf.IMailboxJob]); !ok {
			t.Fatalf("bounded spec constructed %T", q)
		}
	}
}

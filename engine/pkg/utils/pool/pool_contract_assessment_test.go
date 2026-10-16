package pool

import (
	"os"
	"runtime"
	"testing"
)

func TestPerPAssessmentPoppedSlotCleared(t *testing.T) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
	type object struct{ payload []byte }
	p := NewPerPPoolWrapper(1, func() *object { return &object{} }, nil).(*PerPPoolWrapper[*object])
	v := p.Get()
	p.Put(v)
	v = p.Get()
	v.payload = make([]byte, 1<<20)
	runtime.GC()
	pid := procPin()
	local := p.localPools[pid]
	length := len(local.queue)
	cleared := local.queue[:cap(local.queue)][0] == nil
	procUnpin()
	if length != 0 || !cleared {
		t.Fatalf("len=%d, popped slot cleared=%v", length, cleared)
	}
	runtime.KeepAlive(v)
	runtime.KeepAlive(p)
}

func TestPerPAssessmentOwnershipHandoff(t *testing.T) {
	if os.Getenv("PERP_REPRO") == "" {
		t.Skip("opt-in: per-P handoff synchronization is not visible to TSan")
	}
	old := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(old)
	type object struct{ value int }
	p := NewPerPPoolWrapper(8, func() *object { return &object{} }, nil,
		WithPReset(func(v *object) { v.value = 0 }))
	items := make(chan *object)
	done := make(chan bool)
	go func() {
		valid := true
		for i := 1; i <= 10000; i++ {
			v := <-items
			if v.value != i {
				valid = false
			}
			p.Put(v)
		}
		done <- valid
	}()
	for i := 1; i <= 10000; i++ {
		v := p.Get()
		if v.value != 0 {
			t.Error("object not reset")
		}
		v.value = i
		items <- v
	}
	if !<-done {
		t.Fatal("transferred payload changed before return")
	}
}

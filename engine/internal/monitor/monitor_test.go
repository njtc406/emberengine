package monitor

import (
	"github.com/njtc406/emberengine/engine/internal/message/msgenvelope"
	"testing"
	"time"
)

func TestRpcMonitor_Add(t *testing.T) {
	rm := GetRpcMonitor()
	rm.Init()
	rm.Start()
	defer rm.Stop()
	f := msgenvelope.NewMsgEnvelope(nil)
	f.GetMeta().SetTimeout(time.Second)
	rm.Add(f)
}

func TestRpcMonitor_Remove(t *testing.T) {
	rm := GetRpcMonitor()
	rm.Init()
	rm.Start()
	defer rm.Stop()
	f := msgenvelope.NewMsgEnvelope(nil)
	f.GetMeta().SetReqId(1)
	f.GetMeta().SetTimeout(time.Second)
	rm.Add(f)
	rm.Remove(rm.GenSeq())
	nf := rm.Get(rm.GenSeq())
	if nf != nil {
		t.Error("remove failed")
	}
}

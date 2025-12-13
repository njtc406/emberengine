package monitor

import (
	"time"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/rpc/message/msgenvelope"
	"testing"

	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
)

func TestRpcMonitor_Add(t *testing.T) {
	if log.SysLogger == nil {
		log.Init(&log.LoggerConf{Stdout: true, Caller: false, Color: false, Level: "debug"}, true)
	}
	if config.Conf.NodeConf == nil {
		config.Conf.NodeConf = &config.NodeConf{}
	}
	if config.Conf.NodeConf.RpcMonitorConf == nil {
		config.Conf.NodeConf.RpcMonitorConf = &config.RpcMonitorConf{MonitorTimerSize: 1000, MonitorBucketSize: 20}
	}
	timingwheel.Start(time.Millisecond, 64, log.SysLogger)
	defer timingwheel.Stop()

	rm := GetRpcMonitor()
	rm.Init()
	rm.Start()
	defer rm.Stop()
	f := msgenvelope.NewMsgEnvelope(nil)
	f.SetMeta(msgenvelope.NewMeta())
	f.GetMeta().SetTimeout(time.Second)
	rm.Add(f)
}

func TestRpcMonitor_Remove(t *testing.T) {
	if log.SysLogger == nil {
		log.Init(&log.LoggerConf{Stdout: true, Caller: false, Color: false, Level: "debug"}, true)
	}
	if config.Conf.NodeConf == nil {
		config.Conf.NodeConf = &config.NodeConf{}
	}
	if config.Conf.NodeConf.RpcMonitorConf == nil {
		config.Conf.NodeConf.RpcMonitorConf = &config.RpcMonitorConf{MonitorTimerSize: 1000, MonitorBucketSize: 20}
	}
	timingwheel.Start(time.Millisecond, 64, log.SysLogger)
	defer timingwheel.Stop()

	rm := GetRpcMonitor()
	rm.Init()
	rm.Start()
	defer rm.Stop()
	f := msgenvelope.NewMsgEnvelope(nil)
	f.SetMeta(msgenvelope.NewMeta())
	f.GetMeta().SetReqId(1)
	f.GetMeta().SetTimeout(time.Second)
	rm.Add(f)
	rm.Remove(rm.GenSeq())
	nf := rm.Get(rm.GenSeq())
	if nf != nil {
		t.Error("remove failed")
	}
}

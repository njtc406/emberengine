package msgbus

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/monitor"
	"github.com/njtc406/emberengine/engine/pkg/rpc/client"
	"github.com/njtc406/emberengine/engine/pkg/rpc/message/msgenvelope"
	"github.com/njtc406/emberengine/engine/pkg/utils/concurrent"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
)

var benchInitOnce sync.Once

func benchInitRPC() {
	benchInitOnce.Do(func() {
		if log.SysLogger == nil {
			log.Init(&log.LoggerConf{Stdout: false, Caller: false, Color: false, Level: "error"}, true)
		}
		if config.Conf.NodeConf == nil {
			config.Conf.NodeConf = &config.NodeConf{}
		}
		if config.Conf.NodeConf.RpcMonitorConf == nil {
			config.Conf.NodeConf.RpcMonitorConf = &config.RpcMonitorConf{MonitorTimerSize: 10000, MonitorBucketSize: 20}
		}
		timingwheel.Start(time.Millisecond, 64, log.SysLogger)

		rm := monitor.GetRpcMonitor()
		rm.Init()
		rm.Start()
	})
}

type benchMailbox struct {
	handler func(ctx context.Context, ev inf.IEvent)
}

func (m *benchMailbox) PostMessage(ctx context.Context, ev inf.IEvent) error {
	if ev == nil {
		return nil
	}
	if !ev.IsRef() {
		return nil
	}
	defer ev.Release()
	m.handler(ctx, ev)
	return nil
}

type benchRPCPair struct {
	sender   inf.IRpcDispatcher
	receiver inf.IRpcDispatcher
}

func newBenchRPCPair() benchRPCPair {
	benchInitRPC()

	clientPid := actor.NewPID("", "bench", 1, "c1", "bench", "bench-client", 1, def.RpcTypeLocal)
	serverPid := actor.NewPID("", "bench", 1, "s1", "bench", "bench-server", 1, def.RpcTypeLocal)

	var callCount atomic.Uint64
	var asyncCount atomic.Uint64
	var sendCount atomic.Uint64

	clientBox := &benchMailbox{}
	serverBox := &benchMailbox{}

	// client mailbox: only needs to execute concurrent callbacks.
	clientBox.handler = func(ctx context.Context, ev inf.IEvent) {
		if ev.GetType() != event.ServiceConcurrentCallback {
			return
		}
		if e, ok := ev.(*event.Event); ok {
			if cb, ok := e.Data.(concurrent.IConcurrentCallback); ok {
				cb.DoCallback(ctx)
			}
			return
		}
		if cb, ok := ev.(concurrent.IConcurrentCallback); ok {
			cb.DoCallback(ctx)
		}
	}

	// server mailbox: handle local rpc request and (optionally) reply.
	serverBox.handler = func(ctx context.Context, ev inf.IEvent) {
		if ev.GetType() != event.RpcMsg {
			return
		}
		env, ok := ev.(inf.IEnvelope)
		if !ok {
			return
		}

		data := env.GetData()
		if data == nil || data.IsReply() {
			return
		}

		method := data.GetMethod()
		if data.NeedResponse() {
			callCount.Add(1)
			var resp interface{}
			switch method {
			case "RpcSum":
				resp = int(3)
			default:
				resp = int(1)
			}

			respEnv := msgenvelope.NewMsgEnvelope()
			respData := msgenvelope.NewData()
			respData.SetMethod(method)
			respData.SetReply()
			respData.SetResponse(resp)
			respData.SetNeedResponse(false)
			respEnv.SetData(respData)

			respMeta := msgenvelope.NewMeta()
			respMeta.SetReqId(env.GetMeta().GetReqId())
			respMeta.SetSenderPid(serverPid)
			respMeta.SetReceiverPid(clientPid)
			respMeta.SetDispatcher(env.GetMeta().GetDispatcher())
			respEnv.SetMeta(respMeta)

			_ = env.GetMeta().GetDispatcher().Deliver(ctx, respEnv)
			respEnv.Release()
			return
		}

		sendCount.Add(1)
		if method == "Async" {
			asyncCount.Add(1)
		}
	}

	sender := client.NewDispatcher(clientPid, clientBox)
	receiver := client.NewDispatcher(serverPid, serverBox)

	// Keep counters referenced to avoid compiler eliminating work.
	_ = callCount
	_ = asyncCount
	_ = sendCount

	return benchRPCPair{sender: sender, receiver: receiver}
}

func BenchmarkMsgBus_Send_Local_NoPayload(b *testing.B) {
	pair := newBenchRPCPair()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		mb := NewMessageBus(pair.sender, pair.receiver, nil)
		_ = mb.Send(nil, "Noop", nil)
	}
}

func BenchmarkMsgBus_Send_Local_Parallel(b *testing.B) {
	pair := newBenchRPCPair()
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			mb := NewMessageBus(pair.sender, pair.receiver, nil)
			_ = mb.Send(nil, "Noop", nil)
		}
	})
}

func BenchmarkMsgBus_Call_Local_NoOut(b *testing.B) {
	pair := newBenchRPCPair()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		mb := NewMessageBus(pair.sender, pair.receiver, nil)
		_ = mb.Call(nil, "RpcSum", nil, nil)
	}
}

func BenchmarkMsgBus_Call_Local_IntOut(b *testing.B) {
	pair := newBenchRPCPair()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var out int
		mb := NewMessageBus(pair.sender, pair.receiver, nil)
		_ = mb.Call(nil, "RpcSum", nil, &out)
		_ = out
	}
}

func BenchmarkMsgBus_Call_Local_Parallel(b *testing.B) {
	pair := newBenchRPCPair()
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			mb := NewMessageBus(pair.sender, pair.receiver, nil)
			_ = mb.Call(nil, "RpcSum", nil, nil)
		}
	})
}

func BenchmarkMsgBus_AsyncCall_Local_Callback1(b *testing.B) {
	pair := newBenchRPCPair()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		mb := NewMessageBus(pair.sender, pair.receiver, nil)
		_, _ = mb.AsyncCall(context.Background(), "RpcSum", nil, nil, func(ctx context.Context, data interface{}, err error, params ...interface{}) {
			_ = ctx
			_ = data
			_ = err
			_ = params
		})
	}
}

func BenchmarkMsgBus_AsyncCall_Local_Callback1_Parallel(b *testing.B) {
	pair := newBenchRPCPair()
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			mb := NewMessageBus(pair.sender, pair.receiver, nil)
			_, _ = mb.AsyncCall(context.Background(), "RpcSum", nil, nil, func(ctx context.Context, data interface{}, err error, params ...interface{}) {
				_ = ctx
				_ = data
				_ = err
				_ = params
			})
		}
	})
}

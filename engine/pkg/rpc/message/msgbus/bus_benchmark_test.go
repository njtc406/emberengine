package msgbus

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	mbjob "github.com/njtc406/emberengine/engine/pkg/actor/mailbox/job"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/monitor"
	"github.com/njtc406/emberengine/engine/pkg/rpc/client"
	"github.com/njtc406/emberengine/engine/pkg/rpc/client/pool"
	"github.com/njtc406/emberengine/engine/pkg/utils/asynclib"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
)

var benchInitOnce sync.Once
var benchSenderMgr *client.SenderManager
var benchBusFactory *MessageBusFactory
var benchLogger *log.Logger

func benchInitRPC() {
	benchInitOnce.Do(func() {
		if benchLogger == nil {
			l, err := log.NewLogger(&log.LoggerConf{Stdout: false, Caller: false, Color: false, Level: "error"}, true)
			if err != nil {
				panic(err)
			}
			benchLogger = l
		}
		rpcMonitorConf := &config.RpcMonitorConf{MonitorTimerSize: 10000, MonitorBucketSize: 20}
		tw := timingwheel.NewTimingWheel(time.Millisecond, 64, log.NewLoggerX(benchLogger, log.Fields{"pkg": "bench"}))
		tw.Start()
		p, err := asynclib.NewPool(128)
		if err != nil {
			panic(err)
		}

		rm := monitor.NewRpcMonitor().Init(rpcMonitorConf, benchLogger, tw, p)
		rm.Start()
		benchBusFactory = NewMessageBusFactory(10000, benchLogger, rm, def.DefaultRpcTimeout)

		benchSenderMgr = client.NewSenderManager(pool.NewPoolManager(benchLogger), benchLogger, rm, nil)
	})
}

type benchMailbox struct {
	handler func(ctx context.Context, job inf.IMailboxJob)
}

func (m *benchMailbox) PostJob(job inf.IMailboxJob) error {
	if job == nil {
		return nil
	}
	ctx := job.GetContext()
	if ctx == nil {
		ctx = context.Background()
	}
	defer job.Release()
	if m.handler != nil {
		m.handler(ctx, job)
	}
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

	// client mailbox: best-effort drain for callback jobs (and any rpc job dispatched by CallState.Complete).
	clientBox.handler = func(ctx context.Context, j inf.IMailboxJob) {
		switch j.GetType() {
		case def.MailboxJobTypeConcurrentCallback:
			cb := mbjob.GetJobPayloadAs[inf.IConcurrentCallback](j)
			if cb == nil {
				return
			}
			cb.DoCallback(ctx)
			// CallState（来自 monitor 的超时回调）需要归还对象池。
			if st, ok := cb.(*monitor.CallState); ok {
				st.Release()
			}
		case def.MailboxJobTypeRpc:
			// AsyncCall 完成路径当前会 Post 一个 RpcJob（payload 是一个临时 envelope）。
			// 这里不做业务处理，只让 job.Release() 回收 payload。
			_ = mbjob.GetJobPayloadAs[inf.IEnvelope](j)
		default:
			return
		}
	}

	// server mailbox: handle local rpc request and (optionally) reply.
	serverBox.handler = func(ctx context.Context, j inf.IMailboxJob) {
		if j.GetType() != def.MailboxJobTypeRpc {
			return
		}
		env := mbjob.GetJobPayloadAs[inf.IEnvelope](j)
		if env == nil {
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

			// 基准里复用请求 envelope 作为 reply：
			// - local sender 的 reply 路径不会 Release envelope；
			// - request envelope 的最终释放由 server mailbox 的 job.Release() 统一负责。
			data.SetRequest(nil)
			data.SetResponse(resp)
			data.SetError(nil)
			data.SetReply()
			data.SetNeedResponse(false)
			if meta := env.GetMeta(); meta != nil && meta.GetDispatcher() != nil {
				_ = meta.GetDispatcher().DeliverResponse(ctx, env)
			}
			return
		}

		sendCount.Add(1)
		if method == "Async" {
			asyncCount.Add(1)
		}
	}

	sender := client.NewDispatcher(benchSenderMgr, clientPid, clientBox)
	receiver := client.NewDispatcher(benchSenderMgr, serverPid, serverBox)

	// Keep counters referenced to avoid compiler eliminating work.
	_ = &callCount
	_ = &asyncCount
	_ = &sendCount

	return benchRPCPair{sender: sender, receiver: receiver}
}

func BenchmarkMsgBus_Send_Local_NoPayload(b *testing.B) {
	pair := newBenchRPCPair()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		mb := benchBusFactory.New(pair.sender, pair.receiver, nil)
		_ = mb.Send(nil, "Noop", nil)
	}
}

func BenchmarkMsgBus_Send_Local_Parallel(b *testing.B) {
	pair := newBenchRPCPair()
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			mb := benchBusFactory.New(pair.sender, pair.receiver, nil)
			_ = mb.Send(nil, "Noop", nil)
		}
	})
}

func BenchmarkMsgBus_Call_Local_NoOut(b *testing.B) {
	pair := newBenchRPCPair()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		mb := benchBusFactory.New(pair.sender, pair.receiver, nil)
		_ = mb.Call(nil, "RpcSum", nil, nil)
	}
}

func BenchmarkMsgBus_Call_Local_IntOut(b *testing.B) {
	pair := newBenchRPCPair()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var out int
		mb := benchBusFactory.New(pair.sender, pair.receiver, nil)
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
			mb := benchBusFactory.New(pair.sender, pair.receiver, nil)
			_ = mb.Call(nil, "RpcSum", nil, nil)
		}
	})
}

func BenchmarkMsgBus_AsyncCall_Local_Callback1(b *testing.B) {
	pair := newBenchRPCPair()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		mb := benchBusFactory.New(pair.sender, pair.receiver, nil)
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
			mb := benchBusFactory.New(pair.sender, pair.receiver, nil)
			_, _ = mb.AsyncCall(context.Background(), "RpcSum", nil, nil, func(ctx context.Context, data interface{}, err error, params ...interface{}) {
				_ = ctx
				_ = data
				_ = err
				_ = params
			})
		}
	})
}

package core

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/njtc406/emberengine/v2/engine/pkg/actor"
	"github.com/njtc406/emberengine/v2/engine/pkg/actor/mailbox"
	"github.com/njtc406/emberengine/v2/engine/pkg/config"
	corerpc "github.com/njtc406/emberengine/v2/engine/pkg/core/rpc"
	"github.com/njtc406/emberengine/v2/engine/pkg/def"
	inf "github.com/njtc406/emberengine/v2/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/v2/engine/pkg/log"
	"github.com/njtc406/emberengine/v2/engine/pkg/monitor"
	"github.com/njtc406/emberengine/v2/engine/pkg/rpc/client"
	"github.com/njtc406/emberengine/v2/engine/pkg/rpc/client/pool"
	"github.com/njtc406/emberengine/v2/engine/pkg/rpc/message/msgbus"
	"github.com/njtc406/emberengine/v2/engine/pkg/utils/asynclib"
	"github.com/njtc406/emberengine/v2/engine/pkg/utils/timingwheel"
	"github.com/stretchr/testify/require"
)

type boundedRPCAdmission struct {
	id  uint64
	err error
}

// Observe admission without replacing the real service, mailbox, or sender.
type boundedRPCChannel struct {
	inf.IMailboxChannel
	admissions chan boundedRPCAdmission
}

func (c *boundedRPCChannel) PostJob(j inf.IMailboxJob) error {
	id := j.(interface{ GetPayload() inf.IEnvelope }).GetPayload().GetMeta().GetReqId()
	err := c.IMailboxChannel.PostJob(j)
	c.admissions <- boundedRPCAdmission{id: id, err: err}
	return err
}

func awaitBoundedRPC[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case result := <-ch:
		return result
	case <-time.After(2 * time.Second):
		t.Fatal("local RPC acceptance did not complete within 2s")
		var zero T
		return zero
	}
}

// This is deliberately a native (non-race) integration regression: envelope,
// job and CallState pools use the project's native per-P implementation.
func TestLocalRPCBoundedOverloadCompletion(t *testing.T) {
	for _, mode := range []string{"dual", "priority"} {
		t.Run(mode, func(t *testing.T) {
			logger, err := log.NewLogger(&log.LoggerConf{Stdout: true, Level: "error"}, true)
			require.NoError(t, err)
			tw := timingwheel.NewTimingWheel(time.Millisecond, 64, logger)
			tw.Start()
			defer tw.Stop()
			workers, err := asynclib.NewPool(8)
			require.NoError(t, err)
			defer workers.Release()
			rm := monitor.NewRpcMonitor().Init(&config.RpcMonitorConf{MonitorTimerSize: 100, MonitorBucketSize: 8}, logger, tw, workers)
			require.NoError(t, rm.Start())
			defer rm.Stop()
			sm := client.NewSenderManager(pool.NewPoolManager(logger), logger, rm, nil, 1, nil)
			defer sm.Close()
			factory := msgbus.NewMessageBusFactory(16, logger, rm, 10*time.Second)

			svc := &Service{name: "bounded-rpc-receiver"}
			svc.Module.logger, svc.Module.ILoggerX = logger, logger
			svc.methodMgr = corerpc.NewMethodMgr(logger, nil)
			svc.IRpcHandler, err = corerpc.NewHandler(svc).Init(svc.methodMgr)
			require.NoError(t, err)
			svc.initJobHandlers()
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			var executed atomic.Int32
			svc.methodMgr.AddMethodFunc("RpcBoundedEcho", func(_ context.Context, request interface{}) (interface{}, error) {
				value := request.(int)
				executed.Add(1)
				if value == 1 {
					close(entered)
					<-release
				}
				return value * 10, nil
			})
			conf := &config.MailboxConf{
				QueueMode: mode, QueueBackend: "bounded", StopTimeout: time.Second,
				MaxQueueStorageBytes: 1 << 20,
				DualQueueCapacity:    &config.DualQueueCapacityConf{System: 2, User: 2},
				SchedulePolicy: &config.WorkerSchedulePolicy{InitialWorkerNum: 1, VirtualWorkerRate: 24,
					MultiLevelQueueConf: &config.MultiLevelQueueConf{Strategy: def.StrategyFairness,
						PriorityBatches: map[def.Priority]*config.PriorityConfig{
							def.PrioritySys:    {Capacity: 2, BatchSize: 1, Weight: 1},
							def.PriorityHigh:   {Capacity: 2, BatchSize: 1, Weight: 1},
							def.PriorityNormal: {Capacity: 2, BatchSize: 1, Weight: 1},
							def.PriorityUrgent: {Capacity: 2, BatchSize: 1, Weight: 1},
						}},
				},
			}
			svc.mailbox, err = mailbox.NewMailbox(conf, logger, svc, nil)
			require.NoError(t, err)
			svc.mailbox.Start()
			defer svc.mailbox.Stop()
			defer unblock()
			observed := &boundedRPCChannel{IMailboxChannel: svc, admissions: make(chan boundedRPCAdmission, 8)}
			sender := client.NewDispatcher(sm, actor.NewPID("", "local", 1, "caller", "test", "caller", 1, def.RpcTypeLocal), svc)
			receiver := client.NewDispatcher(sm, actor.NewPID("", "local", 1, "receiver", "test", "receiver", 1, def.RpcTypeLocal), observed)
			type result struct {
				value int
				err   error
			}
			call := func(value int) <-chan result {
				done := make(chan result, 1)
				go func() {
					var output int
					err := factory.New(sender, receiver, nil).Call(context.Background(), "RpcBoundedEcho", value, &output)
					done <- result{output, err}
				}()
				return done
			}
			first := call(1)
			ids := []uint64{awaitBoundedRPC(t, observed.admissions).id}
			awaitBoundedRPC(t, entered)
			pending := []<-chan result{first}
			// The single consumer is gated in the handler; exactly two queued calls fit.
			for _, value := range []int{2, 3} {
				pending = append(pending, call(value))
				admission := awaitBoundedRPC(t, observed.admissions)
				require.NoError(t, admission.err)
				ids = append(ids, admission.id)
			}
			refused := call(4)
			admission := awaitBoundedRPC(t, observed.admissions)
			require.ErrorIs(t, admission.err, def.ErrMailboxWorkerIsFull)
			ids = append(ids, admission.id)
			require.ErrorIs(t, awaitBoundedRPC(t, refused).err, def.ErrRPCCallFailed)
			require.Nil(t, rm.Get(admission.id), "refused call must not leave a monitor waiter")
			require.Equal(t, int32(1), executed.Load(), "refused call must not execute")
			unblock()
			for i, done := range pending {
				got := awaitBoundedRPC(t, done)
				require.NoError(t, got.err)
				require.Equal(t, (i+1)*10, got.value)
			}
			// Capacity is reusable and the reply path still completes after overload.
			recovered := call(5)
			admission = awaitBoundedRPC(t, observed.admissions)
			require.NoError(t, admission.err)
			ids = append(ids, admission.id)
			got := awaitBoundedRPC(t, recovered)
			require.NoError(t, got.err)
			require.Equal(t, 50, got.value)
			require.Equal(t, int32(4), executed.Load())
			for _, id := range ids {
				require.NotZero(t, id)
				require.Nil(t, rm.Get(id), "completed RPC waiter %d remains registered", id)
			}
		})
	}
}

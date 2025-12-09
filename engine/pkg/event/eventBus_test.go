package event

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/xcontext"
)

type testService struct {
	name     string
	serverId int32
	pid      *actor.PID
}

func (s *testService) SetName(name string) {
	s.name = name
}

func (s *testService) GetName() string {
	return s.name
}

func (s *testService) GetServerId() int32 {
	return s.serverId
}

func (s *testService) SetPid(pid *actor.PID) {
	s.pid = pid
}
func (s *testService) GetPid() *actor.PID {
	return s.pid
}

func (s *testService) PushEvent(e inf.IEvent) error {
	ev, ok := e.(*Event)
	if !ok {
		return fmt.Errorf("event type is not actor.Event")
	}
	globalEvent := ev.Data.(*actor.Event)
	fmt.Println("service ", s.name, " eventBus receive ", ev.GetType(), " event type:", globalEvent.GetType())
	return nil
}

func TestEventBus(t *testing.T) {
	eb := GetEventBus()
	eb.Init(
		&config.EventBusConf{
			NatsConf: &config.NatsConf{
				EndPoints: []string{"nats://192.168.145.188:4222"},
			},
			ServerPrefix: "server.%d.%d",
			GlobalPrefix: "global.%d",
			ShardCount:   16,
		},
		//nil,
	)
	defer eb.Stop()
	ctx := xcontext.New(nil)
	ctx.SetHeader(def.DefaultDispatcherKey, "111")
	ctx.SetHeader(def.DefaultPriorityKey, def.PriorityNormal)

	service1 := &testService{}
	service2 := &testService{}

	service1.SetName("service1")
	service2.SetName("service2")
	service1.serverId = 2
	service2.serverId = 1

	var wg sync.WaitGroup
	wg.Add(2)

	// goroutine for subscribe
	go func() {
		defer wg.Done()
		fmt.Println("subscribe goroutine start")
		eb.SubscribeGlobal(1, service1)
		eb.SubscribeServer(2, service2)
		fmt.Println("subscribe done")
	}()

	// goroutine for publish
	go func() {
		defer wg.Done()
		// 延迟一下，确保订阅生效（可选，看你的 eventBus 是否支持订阅后立即可用）
		time.Sleep(time.Second * 1)

		fmt.Println("publish goroutine start")
		if err := eb.PublishGlobal(ctx, 1, nil); err != nil {
			t.Error(err)
		}
		if err := eb.PublishServer(ctx, 2, 1, nil); err != nil {
			t.Error(err)
		}
		fmt.Println("publish done")
	}()

	wg.Wait()

	time.Sleep(time.Second)

	eb.UnSubscribeGlobal(1, service1)
	eb.UnSubscribeServer(2, service2)
}

// TestSpecificEvent 测试特定服务事件
func TestSpecificEvent(t *testing.T) {
	eb := GetEventBus()
	eb.Init(
		&config.EventBusConf{
			SpecificPrefix: "specific.%d.%s",
			ShardCount:     16,
		},
	)
	defer eb.Stop()

	ctx := xcontext.New(nil)
	ctx.SetHeader(def.DefaultDispatcherKey, "test-dispatcher")
	ctx.SetHeader(def.DefaultPriorityKey, def.PriorityNormal)

	// 创建测试服务
	targetService := &testService{
		name:     "target-service",
		serverId: 1,
		pid: &actor.PID{
			ServiceUid: "target-service-uid-001",
		},
	}

	subscriber1 := &testService{
		name:     "subscriber1",
		serverId: 2,
		pid: &actor.PID{
			ServiceUid: "subscriber1-uid-001",
		},
	}

	subscriber2 := &testService{
		name:     "subscriber2",
		serverId: 3,
		pid: &actor.PID{
			ServiceUid: "subscriber2-uid-001",
		},
	}

	// subscriber1 和 subscriber2 都订阅 targetService 的事件
	fmt.Println("\n=== 订阅特定服务事件 ===")
	eb.SubscribeSpecific(100, targetService.GetPid().GetServiceUid(), subscriber1)
	eb.SubscribeSpecific(100, targetService.GetPid().GetServiceUid(), subscriber2)
	fmt.Println("subscriber1 和 subscriber2 已订阅 targetService 的事件")

	// 等待订阅生效
	time.Sleep(100 * time.Millisecond)

	// 发布特定服务事件
	fmt.Println("\n=== 发布特定服务事件 ===")
	if err := eb.PublishSpecificLocal(ctx, 100, targetService.GetPid().GetServiceUid(), nil); err != nil {
		t.Errorf("发布特定服务事件失败: %v", err)
	}
	fmt.Println("targetService 发布事件完成")

	// 等待事件处理
	time.Sleep(500 * time.Millisecond)

	// 取消订阅
	fmt.Println("\n=== 取消订阅 ===")
	eb.UnSubscribeSpecific(100, targetService.GetPid().GetServiceUid(), subscriber1)
	eb.UnSubscribeSpecific(100, targetService.GetPid().GetServiceUid(), subscriber2)
	fmt.Println("取消订阅完成")

	fmt.Println("\n=== 测试完成 ===")
}

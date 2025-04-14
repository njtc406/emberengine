package event

import (
	"fmt"
	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"sync"
	"testing"
	"time"
)

type testService struct {
	name     string
	serverId int32
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

func (s *testService) PushEvent(e inf.IEvent) error {
	ev, ok := e.(*Event)
	if !ok {
		return fmt.Errorf("event type is not actor.Event")
	}
	globalEvent := ev.Data.(*actor.Event)
	fmt.Println("service ", s.name, " eventBus receive ", ev.GetType(), " event type:", globalEvent.GetEventType())
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
	header := dto.Header{
		"traceId":     "123",
		"DispatchKey": "111",
		"Priority":    "0",
	}

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
		if err := eb.PublishGlobal(1, nil, header); err != nil {
			t.Error(err)
		}
		if err := eb.PublishServer(2, 1, nil, header); err != nil {
			t.Error(err)
		}
		fmt.Println("publish done")
	}()

	wg.Wait()

	time.Sleep(time.Second)

	eb.UnSubscribeGlobal(1, service1)
	eb.UnSubscribeServer(2, service2)
}

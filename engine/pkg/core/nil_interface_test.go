package core

import (
	"errors"
	"testing"

	"github.com/njtc406/emberengine/v2/engine/pkg/actor/mailbox"
	"github.com/njtc406/emberengine/v2/engine/pkg/config"
	"github.com/njtc406/emberengine/v2/engine/pkg/def"
	"github.com/njtc406/emberengine/v2/engine/pkg/event"
	inf "github.com/njtc406/emberengine/v2/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/v2/engine/pkg/log"
)

func TestModuleNilInterfaceGetters(t *testing.T) {
	var m Module
	getters := []struct {
		name string
		get  func() any
	}{
		{"event handler", func() any { return m.GetEventHandler() }},
		{"event registry", func() any { return m.GetEventHandlerRegistry() }},
		{"event processor", func() any { return m.GetEventProcessor() }},
	}
	for _, tt := range getters {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.get(); got != nil {
				t.Fatalf("uninitialized component = %T, want nil interface", got)
			}
		})
	}
}

func TestModuleInterfaceGettersLifecycle(t *testing.T) {
	processor := event.NewTrigger()
	handler := event.NewTriggerHandler()
	handler.Init(processor)
	m := &Module{eventHandler: handler}
	if m.GetBaseModule() != m || m.GetEventHandler() != handler ||
		m.GetEventHandlerRegistry() != handler || m.GetEventProcessor() != processor {
		t.Fatal("getters must preserve initialized components")
	}
	m.reset()
	if m.GetEventHandler() != nil || m.GetEventHandlerRegistry() != nil || m.GetEventProcessor() != nil {
		t.Fatal("reset module must return nil event interfaces")
	}
}

func TestNilModuleGetBaseModule(t *testing.T) {
	var m *Module
	if got := m.GetBaseModule(); got != nil {
		t.Fatalf("nil base module = %T, want nil interface", got)
	}
}

func TestUninitializedModuleAddReturnsError(t *testing.T) {
	var m Module
	if _, err := m.AddModule(&Module{}); !errors.Is(err, def.ErrModuleNotInitialized) {
		t.Fatalf("AddModule error = %v, want ErrModuleNotInitialized", err)
	}
}

func TestServiceNilMailboxInterface(t *testing.T) {
	var s Service
	var service inf.IService = &s
	if got := service.GetMailbox(); got != nil {
		t.Fatalf("uninitialized mailbox = %T, want nil interface", got)
	}
	s.mailbox = &mailbox.Mailbox{}
	if service.GetMailbox() != s.mailbox {
		t.Fatal("getters must preserve initialized mailbox")
	}
}

type nilLoggerNodeContext struct {
	inf.INodeContext
	logger *log.Logger
}

func (n *nilLoggerNodeContext) GetLogger() log.ILoggerX { return n.logger }

func TestInitLoggerWithMissingNodeLogger(t *testing.T) {
	for _, tt := range []struct {
		name string
		ctx  inf.INodeContext
	}{
		{"no context", nil},
		{"typed nil logger", &nilLoggerNodeContext{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var s Service
			s.SetNodeContext(tt.ctx)
			if err := s.initLogger(&config.ServiceInitConf{LogConf: &config.ServiceLogConf{}}); err != nil {
				t.Fatal(err)
			}
			logger, ok := s.GetLogger().(*log.Logger)
			if !ok || logger == nil || s.ILoggerX == nil {
				t.Fatal("missing node logger must create a usable fallback logger")
			}
			t.Cleanup(func() { _ = logger.Close() })
		})
	}
}

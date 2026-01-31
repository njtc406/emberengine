package event

import (
	"context"
	"fmt"
	"sync"

	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

var _ inf.IEventHandler = (*Handler)(nil)

type regScope uint8

const (
	scopeLocal regScope = iota
	scopeGlobal
	scopeServer
	scopeSpecific
)

type regInfo struct {
	scope      regScope
	eventType  def.EventType
	serviceUid string
	name       string
}

type Handler struct {
	mu      sync.RWMutex
	trigger inf.IEventProcessor
	regs    []regInfo
}

func NewTriggerHandler() *Handler {
	return &Handler{}
}

func (h *Handler) Init(trigger inf.IEventProcessor) {
	h.trigger = trigger
	h.regs = make([]regInfo, 0)
}

func (h *Handler) GetTrigger() inf.IEventProcessor {
	return h.trigger
}

func (h *Handler) RegisterEvent(eventType def.EventType, name string, callback inf.EventHandlerAny) error {
	if h.trigger == nil {
		return fmt.Errorf("trigger is nil")
	}
	h.mu.Lock()
	h.regs = append(h.regs, regInfo{scope: scopeLocal, eventType: eventType, name: name})
	h.mu.Unlock()
	h.trigger.BindHandler(eventType, name, h, callback)
	return nil
}

func (h *Handler) UnregisterEvent(eventType def.EventType, name string) {
	if h.trigger == nil {
		return
	}
	h.mu.Lock()
	removeFirstReg(&h.regs, func(r regInfo) bool { return r.scope == scopeLocal && r.eventType == eventType && r.name == name })
	h.mu.Unlock()
	h.trigger.UnbindHandler(eventType, name, h)
}

func (h *Handler) UnregisterAll() {
	if h.trigger == nil {
		return
	}

	h.mu.Lock()
	regs := make([]regInfo, len(h.regs))
	copy(regs, h.regs)
	h.regs = h.regs[:0]
	h.mu.Unlock()

	for _, r := range regs {
		switch r.scope {
		case scopeLocal:
			h.trigger.UnbindHandler(r.eventType, r.name, h)
		case scopeGlobal:
			h.trigger.UnbindGlobalHandler(r.eventType, r.name, h)
		case scopeServer:
			h.trigger.UnbindServerHandler(r.eventType, r.name, h)
		case scopeSpecific:
			h.trigger.UnbindSpecificHandler(r.eventType, r.serviceUid, r.name, h)
		}
	}
}

func (h *Handler) Trigger(ctx context.Context, eventType def.EventType, data any) {
	if h.trigger == nil {
		return
	}
	h.trigger.Trigger(ctx, eventType, data)
}

func (h *Handler) GetRegisteredEvents() map[def.EventType][]string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	out := make(map[def.EventType][]string)
	for _, r := range h.regs {
		out[r.eventType] = append(out[r.eventType], r.name)
	}
	return out
}

func (h *Handler) Destroy() {
	h.UnregisterAll()
	h.trigger = nil
}

func (h *Handler) RegisterGlobalEvent(eventType def.EventType, name string, callback inf.EventHandlerAny) error {
	if h.trigger == nil {
		return fmt.Errorf("trigger is nil")
	}
	h.mu.Lock()
	h.regs = append(h.regs, regInfo{scope: scopeGlobal, eventType: eventType, name: name})
	h.mu.Unlock()
	h.trigger.BindGlobalHandler(eventType, name, h, callback)
	return nil
}

func (h *Handler) UnregisterGlobalEvent(eventType def.EventType, name string) {
	if h.trigger == nil {
		return
	}
	h.mu.Lock()
	removeFirstReg(&h.regs, func(r regInfo) bool { return r.scope == scopeGlobal && r.eventType == eventType && r.name == name })
	h.mu.Unlock()
	h.trigger.UnbindGlobalHandler(eventType, name, h)
}

func (h *Handler) RegisterServerEvent(eventType def.EventType, name string, callback inf.EventHandlerAny) error {
	if h.trigger == nil {
		return fmt.Errorf("trigger is nil")
	}
	h.mu.Lock()
	h.regs = append(h.regs, regInfo{scope: scopeServer, eventType: eventType, name: name})
	h.mu.Unlock()
	h.trigger.BindServerHandler(eventType, name, h, callback)
	return nil
}

func (h *Handler) UnregisterServerEvent(eventType def.EventType, name string) {
	if h.trigger == nil {
		return
	}
	h.mu.Lock()
	removeFirstReg(&h.regs, func(r regInfo) bool { return r.scope == scopeServer && r.eventType == eventType && r.name == name })
	h.mu.Unlock()
	h.trigger.UnbindServerHandler(eventType, name, h)
}

func (h *Handler) RegisterSpecificEvent(eventType def.EventType, serviceUid string, name string, callback inf.EventHandlerAny) error {
	if h.trigger == nil {
		return fmt.Errorf("trigger is nil")
	}
	h.mu.Lock()
	h.regs = append(h.regs, regInfo{scope: scopeSpecific, eventType: eventType, serviceUid: serviceUid, name: name})
	h.mu.Unlock()
	h.trigger.BindSpecificHandler(eventType, serviceUid, name, h, callback)
	return nil
}

func (h *Handler) UnregisterSpecificEvent(eventType def.EventType, serviceUid string, name string) {
	if h.trigger == nil {
		return
	}
	h.mu.Lock()
	removeFirstReg(&h.regs, func(r regInfo) bool {
		return r.scope == scopeSpecific && r.eventType == eventType && r.serviceUid == serviceUid && r.name == name
	})
	h.mu.Unlock()
	h.trigger.UnbindSpecificHandler(eventType, serviceUid, name, h)
}

func removeFirstReg(regs *[]regInfo, match func(regInfo) bool) {
	in := *regs
	for i, r := range in {
		if match(r) {
			*regs = append(in[:i], in[i+1:]...)
			return
		}
	}
}

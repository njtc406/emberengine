// Package event
// @Title  同步事件触发器（服务内 + 集群事件）
// @Description  服务内同步触发；集群事件通过EventBus订阅，投递job后在服务内触发对应handler
// @Author  yr  2026/1/31
// @Update  yr  2026/1/31
package event

import (
	"context"
	"sync"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"google.golang.org/protobuf/proto"
)

var _ inf.ITrigger = (*Trigger)(nil)

type triggerEntry struct {
	name     string
	owner    inf.ITriggerHandler
	callback inf.TriggerCallbackAny
}

type specificKey struct {
	eventType  def.EventType
	serviceUid string
}

// Trigger 服务级别同步触发器（一个 service 一个）
// - 本地事件：Trigger(ctx, eventType, data)
// - 集群事件：注册时会Subscribe到EventBus；当服务收到EventBusJob时，应调用 EventHandler(ctx, ev)
type Trigger struct {
	mu       sync.RWMutex
	listener inf.IListener

	local    map[def.EventType][]*triggerEntry
	global   map[def.EventType][]*triggerEntry
	server   map[def.EventType][]*triggerEntry
	specific map[specificKey][]*triggerEntry

	// 订阅引用计数，避免重复订阅与提前取消
	globalSubCnt   map[def.EventType]int
	serverSubCnt   map[def.EventType]int
	specificSubCnt map[specificKey]int
}

func NewTrigger() *Trigger {
	return &Trigger{}
}

func (t *Trigger) Init() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.local = make(map[def.EventType][]*triggerEntry)
	t.global = make(map[def.EventType][]*triggerEntry)
	t.server = make(map[def.EventType][]*triggerEntry)
	t.specific = make(map[specificKey][]*triggerEntry)

	t.globalSubCnt = make(map[def.EventType]int)
	t.serverSubCnt = make(map[def.EventType]int)
	t.specificSubCnt = make(map[specificKey]int)
}

func (t *Trigger) SetListener(listener inf.IListener) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.listener = listener
}

func (t *Trigger) HasHandler(eventType def.EventType) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.local[eventType])+len(t.global[eventType])+len(t.server[eventType]) > 0
}

func (t *Trigger) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()

	// 尽量取消订阅
	if t.listener != nil {
		for et, cnt := range t.globalSubCnt {
			if cnt > 0 {
				GetEventBus().UnSubscribeGlobal(et, t.listener)
			}
		}
		for et, cnt := range t.serverSubCnt {
			if cnt > 0 {
				GetEventBus().UnSubscribeServer(et, t.listener)
			}
		}
		for k, cnt := range t.specificSubCnt {
			if cnt > 0 {
				GetEventBus().UnSubscribeSpecific(k.eventType, k.serviceUid, t.listener)
			}
		}
	}

	t.local = make(map[def.EventType][]*triggerEntry)
	t.global = make(map[def.EventType][]*triggerEntry)
	t.server = make(map[def.EventType][]*triggerEntry)
	t.specific = make(map[specificKey][]*triggerEntry)

	t.globalSubCnt = make(map[def.EventType]int)
	t.serverSubCnt = make(map[def.EventType]int)
	t.specificSubCnt = make(map[specificKey]int)
}

func (t *Trigger) BindHandler(eventType def.EventType, name string, handler inf.ITriggerHandler, callback inf.TriggerCallbackAny) {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry := &triggerEntry{name: name, owner: handler, callback: callback}
	t.local[eventType] = append(t.local[eventType], entry)
}

func (t *Trigger) UnbindHandler(eventType def.EventType, name string, handler inf.ITriggerHandler) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.local[eventType] = removeEntry(t.local[eventType], name, handler)
}

func (t *Trigger) BindGlobalHandler(eventType def.EventType, name string, handler inf.ITriggerHandler, callback inf.TriggerCallbackAny) {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry := &triggerEntry{name: name, owner: handler, callback: callback}
	t.global[eventType] = append(t.global[eventType], entry)

	if t.listener != nil {
		if t.globalSubCnt[eventType] == 0 {
			GetEventBus().SubscribeGlobal(eventType, t.listener)
		}
		t.globalSubCnt[eventType]++
	}
}

func (t *Trigger) UnbindGlobalHandler(eventType def.EventType, name string, handler inf.ITriggerHandler) {
	t.mu.Lock()
	defer t.mu.Unlock()

	before := len(t.global[eventType])
	t.global[eventType] = removeEntry(t.global[eventType], name, handler)
	after := len(t.global[eventType])

	if t.listener != nil && before != after {
		t.globalSubCnt[eventType]--
		if t.globalSubCnt[eventType] <= 0 {
			delete(t.globalSubCnt, eventType)
			GetEventBus().UnSubscribeGlobal(eventType, t.listener)
		}
	}
}

func (t *Trigger) BindServerHandler(eventType def.EventType, name string, handler inf.ITriggerHandler, callback inf.TriggerCallbackAny) {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry := &triggerEntry{name: name, owner: handler, callback: callback}
	t.server[eventType] = append(t.server[eventType], entry)

	if t.listener != nil {
		if t.serverSubCnt[eventType] == 0 {
			GetEventBus().SubscribeServer(eventType, t.listener)
		}
		t.serverSubCnt[eventType]++
	}
}

func (t *Trigger) UnbindServerHandler(eventType def.EventType, name string, handler inf.ITriggerHandler) {
	t.mu.Lock()
	defer t.mu.Unlock()

	before := len(t.server[eventType])
	t.server[eventType] = removeEntry(t.server[eventType], name, handler)
	after := len(t.server[eventType])

	if t.listener != nil && before != after {
		t.serverSubCnt[eventType]--
		if t.serverSubCnt[eventType] <= 0 {
			delete(t.serverSubCnt, eventType)
			GetEventBus().UnSubscribeServer(eventType, t.listener)
		}
	}
}

func (t *Trigger) BindSpecificHandler(eventType def.EventType, serviceUid string, name string, handler inf.ITriggerHandler, callback inf.TriggerCallbackAny) {
	t.mu.Lock()
	defer t.mu.Unlock()

	k := specificKey{eventType: eventType, serviceUid: serviceUid}
	entry := &triggerEntry{name: name, owner: handler, callback: callback}
	t.specific[k] = append(t.specific[k], entry)

	if t.listener != nil {
		if t.specificSubCnt[k] == 0 {
			GetEventBus().SubscribeSpecific(eventType, serviceUid, t.listener)
		}
		t.specificSubCnt[k]++
	}
}

func (t *Trigger) UnbindSpecificHandler(eventType def.EventType, serviceUid string, name string, handler inf.ITriggerHandler) {
	t.mu.Lock()
	defer t.mu.Unlock()

	k := specificKey{eventType: eventType, serviceUid: serviceUid}
	before := len(t.specific[k])
	t.specific[k] = removeEntry(t.specific[k], name, handler)
	after := len(t.specific[k])

	if after == 0 {
		delete(t.specific, k)
	}

	if t.listener != nil && before != after {
		t.specificSubCnt[k]--
		if t.specificSubCnt[k] <= 0 {
			delete(t.specificSubCnt, k)
			GetEventBus().UnSubscribeSpecific(eventType, serviceUid, t.listener)
		}
	}
}

// Trigger 本地同步触发（不经过EventBus）
func (t *Trigger) Trigger(ctx context.Context, eventType def.EventType, data any) {
	entries := t.snapshotLocal(eventType)
	for _, e := range entries {
		t.safeExec(e, ctx, eventType, data)
	}
}

// EventHandler 由服务的 MailboxJobTypeEvent handler 调用
// 这里会把 *actor.Event 作为 data 传给已注册的集群事件回调（wrapper里可自动反序列化到具体类型）
func (t *Trigger) EventHandler(ctx context.Context, ev *actor.Event) {
	if ev == nil {
		return
	}
	et := def.EventType(ev.GetType())

	entries := t.snapshotCluster(et, ev.GetServiceUid())
	for _, e := range entries {
		t.safeExec(e, ctx, et, ev)
	}
}

func (t *Trigger) PublishGlobal(ctx context.Context, eventType def.EventType, data proto.Message) error {
	return GetEventBus().PublishGlobal(ctx, eventType, data)
}

func (t *Trigger) PublishServer(ctx context.Context, eventType def.EventType, data proto.Message) error {
	t.mu.RLock()
	listener := t.listener
	t.mu.RUnlock()
	if listener == nil {
		return GetEventBus().PublishServer(ctx, eventType, 0, data)
	}
	return GetEventBus().PublishServer(ctx, eventType, listener.GetPartition(), data)
}

func (t *Trigger) PublishSpecific(ctx context.Context, eventType def.EventType, serviceUid string, data proto.Message) error {
	return GetEventBus().PublishSpecific(ctx, eventType, serviceUid, data)
}

func (t *Trigger) snapshotLocal(eventType def.EventType) []*triggerEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return cloneEntries(t.local[eventType])
}

func (t *Trigger) snapshotCluster(eventType def.EventType, targetServiceUid string) []*triggerEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var out []*triggerEntry
	out = append(out, t.global[eventType]...)
	out = append(out, t.server[eventType]...)
	if targetServiceUid != "" {
		k := specificKey{eventType: eventType, serviceUid: targetServiceUid}
		out = append(out, t.specific[k]...)
	}
	return cloneEntries(out)
}

func (t *Trigger) safeExec(entry *triggerEntry, ctx context.Context, eventType def.EventType, data any) {
	defer func() {
		if err := recover(); err != nil {
			log.SysLogger.Errorf("trigger handler panic: eventType=%d, name=%s, err=%v", eventType, entry.name, err)
		}
	}()
	entry.callback(ctx, eventType, data)
}

func cloneEntries(in []*triggerEntry) []*triggerEntry {
	if len(in) == 0 {
		return nil
	}
	out := make([]*triggerEntry, len(in))
	copy(out, in)
	return out
}

func removeEntry(in []*triggerEntry, name string, owner inf.ITriggerHandler) []*triggerEntry {
	if len(in) == 0 {
		return in
	}
	out := in[:0]
	for _, e := range in {
		if e == nil {
			continue
		}
		if e.name == name && e.owner == owner {
			continue
		}
		out = append(out, e)
	}
	return out
}

// ================= module 级 TriggerHandler =================

var _ inf.ITriggerHandler = (*TriggerHandler)(nil)

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

type TriggerHandler struct {
	mu      sync.RWMutex
	trigger inf.ITrigger
	regs    []regInfo
}

func NewTriggerHandler() *TriggerHandler {
	return &TriggerHandler{}
}

func (h *TriggerHandler) Init(trigger inf.ITrigger) {
	h.trigger = trigger
	h.regs = make([]regInfo, 0)
}

func (h *TriggerHandler) GetTrigger() inf.ITrigger {
	return h.trigger
}

func (h *TriggerHandler) RegisterAny(eventType def.EventType, name string, callback inf.TriggerCallbackAny) {
	if h.trigger == nil {
		return
	}
	h.mu.Lock()
	h.regs = append(h.regs, regInfo{scope: scopeLocal, eventType: eventType, name: name})
	h.mu.Unlock()
	h.trigger.BindHandler(eventType, name, h, callback)
}

func (h *TriggerHandler) Unregister(eventType def.EventType, name string) {
	if h.trigger == nil {
		return
	}
	h.mu.Lock()
	removeFirstReg(&h.regs, func(r regInfo) bool { return r.scope == scopeLocal && r.eventType == eventType && r.name == name })
	h.mu.Unlock()
	h.trigger.UnbindHandler(eventType, name, h)
}

func (h *TriggerHandler) UnregisterAll() {
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

func (h *TriggerHandler) Trigger(ctx context.Context, eventType def.EventType, data any) {
	if h.trigger == nil {
		return
	}
	h.trigger.Trigger(ctx, eventType, data)
}

func (h *TriggerHandler) GetRegisteredEvents() map[def.EventType][]string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	out := make(map[def.EventType][]string)
	for _, r := range h.regs {
		out[r.eventType] = append(out[r.eventType], r.name)
	}
	return out
}

func (h *TriggerHandler) Destroy() {
	h.UnregisterAll()
	h.trigger = nil
}

func (h *TriggerHandler) RegisterGlobalAny(eventType def.EventType, name string, callback inf.TriggerCallbackAny) {
	if h.trigger == nil {
		return
	}
	h.mu.Lock()
	h.regs = append(h.regs, regInfo{scope: scopeGlobal, eventType: eventType, name: name})
	h.mu.Unlock()
	h.trigger.BindGlobalHandler(eventType, name, h, callback)
}

func (h *TriggerHandler) UnregisterGlobal(eventType def.EventType, name string) {
	if h.trigger == nil {
		return
	}
	h.mu.Lock()
	removeFirstReg(&h.regs, func(r regInfo) bool { return r.scope == scopeGlobal && r.eventType == eventType && r.name == name })
	h.mu.Unlock()
	h.trigger.UnbindGlobalHandler(eventType, name, h)
}

func (h *TriggerHandler) RegisterServerAny(eventType def.EventType, name string, callback inf.TriggerCallbackAny) {
	if h.trigger == nil {
		return
	}
	h.mu.Lock()
	h.regs = append(h.regs, regInfo{scope: scopeServer, eventType: eventType, name: name})
	h.mu.Unlock()
	h.trigger.BindServerHandler(eventType, name, h, callback)
}

func (h *TriggerHandler) UnregisterServer(eventType def.EventType, name string) {
	if h.trigger == nil {
		return
	}
	h.mu.Lock()
	removeFirstReg(&h.regs, func(r regInfo) bool { return r.scope == scopeServer && r.eventType == eventType && r.name == name })
	h.mu.Unlock()
	h.trigger.UnbindServerHandler(eventType, name, h)
}

func (h *TriggerHandler) RegisterSpecificAny(eventType def.EventType, serviceUid string, name string, callback inf.TriggerCallbackAny) {
	if h.trigger == nil {
		return
	}
	h.mu.Lock()
	h.regs = append(h.regs, regInfo{scope: scopeSpecific, eventType: eventType, serviceUid: serviceUid, name: name})
	h.mu.Unlock()
	h.trigger.BindSpecificHandler(eventType, serviceUid, name, h, callback)
}

func (h *TriggerHandler) UnregisterSpecific(eventType def.EventType, serviceUid string, name string) {
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

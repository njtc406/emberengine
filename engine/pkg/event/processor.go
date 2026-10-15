// Package event
// @Title  同步事件触发器（服务内 + 集群事件）
// @Description  服务内同步触发；集群事件通过EventBus订阅，投递job后在服务内触发对应handler
// @Author  yr  2026/1/31
// @Update  yr  2026/1/31
package event

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/codec"
	"google.golang.org/protobuf/proto"
)

var _ inf.IEventProcessor = (*Processor)(nil)

type callbackEntry struct {
	name string
	cb   inf.EventHandlerAny
}

type specificKey struct {
	eventType  def.EventType
	serviceUid string
}

// Processor 服务级别同步触发器（一个 service 一个）
// - 本地事件：Processor(ctx, eventType, data)
// - 集群事件：注册时会Subscribe到EventBus；当服务收到EventBusJob时，应调用 EventHandler(ctx, ev)
type Processor struct {
	mu       sync.RWMutex
	listener inf.IListener
	bus      *Bus
	logger   log.ILoggerX

	// 结构：map[事件类型]map[所属handler]map[回调名]entry
	// 这样既能支持同一 module(handler) 多个 name 的注册，也能按 handler+name 精准解绑。
	local    map[def.EventType]map[inf.IEventHandler]map[string]inf.EventHandlerAny
	global   map[def.EventType]map[inf.IEventHandler]map[string]inf.EventHandlerAny
	server   map[def.EventType]map[inf.IEventHandler]map[string]inf.EventHandlerAny
	specific map[specificKey]map[inf.IEventHandler]map[string]inf.EventHandlerAny

	// 订阅引用计数，避免重复订阅与提前取消
	globalSubCnt   map[def.EventType]int
	serverSubCnt   map[def.EventType]int
	specificSubCnt map[specificKey]int
}

func NewTrigger() *Processor {
	return &Processor{}
}

func (t *Processor) Init(listener inf.IListener) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.local = make(map[def.EventType]map[inf.IEventHandler]map[string]inf.EventHandlerAny)
	t.global = make(map[def.EventType]map[inf.IEventHandler]map[string]inf.EventHandlerAny)
	t.server = make(map[def.EventType]map[inf.IEventHandler]map[string]inf.EventHandlerAny)
	t.specific = make(map[specificKey]map[inf.IEventHandler]map[string]inf.EventHandlerAny)

	t.globalSubCnt = make(map[def.EventType]int)
	t.serverSubCnt = make(map[def.EventType]int)
	t.specificSubCnt = make(map[specificKey]int)

	t.listener = listener
	if loggerProvider, ok := listener.(interface{ GetLogger() log.ILoggerX }); ok {
		t.logger = loggerProvider.GetLogger()
	}
}

func (t *Processor) SetEventBus(bus *Bus) {
	t.mu.Lock()
	t.bus = bus
	t.mu.Unlock()
}

func (t *Processor) HasHandler(eventType def.EventType) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, byName := range t.local[eventType] {
		if len(byName) > 0 {
			return true
		}
	}
	for _, byName := range t.global[eventType] {
		if len(byName) > 0 {
			return true
		}
	}
	for _, byName := range t.server[eventType] {
		if len(byName) > 0 {
			return true
		}
	}
	for k, byHandler := range t.specific {
		if k.eventType != eventType {
			continue
		}
		for _, byName := range byHandler {
			if len(byName) > 0 {
				return true
			}
		}
	}
	return false
}

func (t *Processor) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()

	// 尽量取消订阅
	if t.listener != nil && t.bus != nil {
		for et, cnt := range t.globalSubCnt {
			if cnt > 0 {
				t.bus.UnSubscribeGlobal(et, t.listener)
			}
		}
		for et, cnt := range t.serverSubCnt {
			if cnt > 0 {
				t.bus.UnSubscribeServer(et, t.listener)
			}
		}
		for k, cnt := range t.specificSubCnt {
			if cnt > 0 {
				t.bus.UnSubscribeSpecific(k.eventType, k.serviceUid, t.listener)
			}
		}
	}

	t.local = make(map[def.EventType]map[inf.IEventHandler]map[string]inf.EventHandlerAny)
	t.global = make(map[def.EventType]map[inf.IEventHandler]map[string]inf.EventHandlerAny)
	t.server = make(map[def.EventType]map[inf.IEventHandler]map[string]inf.EventHandlerAny)
	t.specific = make(map[specificKey]map[inf.IEventHandler]map[string]inf.EventHandlerAny)

	t.globalSubCnt = make(map[def.EventType]int)
	t.serverSubCnt = make(map[def.EventType]int)
	t.specificSubCnt = make(map[specificKey]int)
}

func (t *Processor) BindHandler(eventType def.EventType, name string, handler inf.IEventHandler, callback inf.EventHandlerAny) {
	t.mu.Lock()
	defer t.mu.Unlock()
	byHandler := t.local[eventType]
	if byHandler == nil {
		byHandler = make(map[inf.IEventHandler]map[string]inf.EventHandlerAny)
		t.local[eventType] = byHandler
	}
	byName := byHandler[handler]
	if byName == nil {
		byName = make(map[string]inf.EventHandlerAny)
		byHandler[handler] = byName
	}
	byName[name] = callback
}

func (t *Processor) UnbindHandler(eventType def.EventType, name string, handler inf.IEventHandler) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if byHandler := t.local[eventType]; byHandler != nil {
		if byName := byHandler[handler]; byName != nil {
			delete(byName, name)
			if len(byName) == 0 {
				delete(byHandler, handler)
			}
		}
		if len(byHandler) == 0 {
			delete(t.local, eventType)
		}
	}
}

// ========== 泛型绑定函数（包级别，用于类型安全注册） ==========

// BindHandler 泛型版本，将TriggerCallback[T]包装为TriggerCallbackAny并绑定
func BindHandler[T any](t inf.IEventProcessor, eventType def.EventType, name string, handler inf.IEventHandler, callback inf.EventHandler[T]) {
	wrapped := func(ctx context.Context, data any) error {
		return callback(ctx, data.(T))
	}
	t.BindHandler(eventType, name, handler, wrapped)
}

// BindGlobalHandler 泛型版本，将TriggerCallback[T]包装为TriggerCallbackAny并绑定全局事件
func BindGlobalHandler[T any](t inf.IEventProcessor, eventType def.EventType, name string, handler inf.IEventHandler, callback inf.EventHandler[T]) {
	wrapped := func(ctx context.Context, data any) error {
		return callback(ctx, data.(T))
	}
	t.BindGlobalHandler(eventType, name, handler, wrapped)
}

// BindServerHandler 泛型版本，将TriggerCallback[T]包装为TriggerCallbackAny并绑定服务器事件
func BindServerHandler[T any](t inf.IEventProcessor, eventType def.EventType, name string, handler inf.IEventHandler, callback inf.EventHandler[T]) {
	wrapped := func(ctx context.Context, data any) error {
		return callback(ctx, data.(T))
	}
	t.BindServerHandler(eventType, name, handler, wrapped)
}

// BindSpecificHandler 泛型版本，将TriggerCallback[T]包装为TriggerCallbackAny并绑定特定服务事件
func BindSpecificHandler[T any](t inf.IEventProcessor, eventType def.EventType, serviceUid string, name string, handler inf.IEventHandler, callback inf.EventHandler[T]) {
	wrapped := func(ctx context.Context, data any) error {
		return callback(ctx, data.(T))
	}
	t.BindSpecificHandler(eventType, serviceUid, name, handler, wrapped)
}

// UnbindHandler 包装函数，取消绑定本地事件处理器
func UnbindHandler(t inf.IEventProcessor, eventType def.EventType, name string, handler inf.IEventHandler) {
	t.UnbindHandler(eventType, name, handler)
}

// UnbindGlobalHandler 包装函数，取消绑定全局事件处理器
func UnbindGlobalHandler(t inf.IEventProcessor, eventType def.EventType, name string, handler inf.IEventHandler) {
	t.UnbindGlobalHandler(eventType, name, handler)
}

// UnbindServerHandler 包装函数，取消绑定服务器事件处理器
func UnbindServerHandler(t inf.IEventProcessor, eventType def.EventType, name string, handler inf.IEventHandler) {
	t.UnbindServerHandler(eventType, name, handler)
}

// UnbindSpecificHandler 包装函数，取消绑定特定服务事件处理器
func UnbindSpecificHandler(t inf.IEventProcessor, eventType def.EventType, serviceUid string, name string, handler inf.IEventHandler) {
	t.UnbindSpecificHandler(eventType, serviceUid, name, handler)
}

func (t *Processor) BindGlobalHandler(eventType def.EventType, name string, handler inf.IEventHandler, callback inf.EventHandlerAny) {
	t.mu.Lock()
	defer t.mu.Unlock()
	byHandler := t.global[eventType]
	if byHandler == nil {
		byHandler = make(map[inf.IEventHandler]map[string]inf.EventHandlerAny)
		t.global[eventType] = byHandler
	}
	byName := byHandler[handler]
	if byName == nil {
		byName = make(map[string]inf.EventHandlerAny)
		byHandler[handler] = byName
	}
	_, existed := byName[name]
	byName[name] = callback

	if t.listener != nil && t.bus != nil && !existed {
		if t.globalSubCnt[eventType] == 0 {
			t.bus.SubscribeGlobal(eventType, t.listener)
		}
		t.globalSubCnt[eventType]++
	}
}

func (t *Processor) UnbindGlobalHandler(eventType def.EventType, name string, handler inf.IEventHandler) {
	t.mu.Lock()
	defer t.mu.Unlock()
	byHandler := t.global[eventType]
	if byHandler == nil {
		return
	}
	byName := byHandler[handler]
	if byName == nil {
		return
	}
	if _, exists := byName[name]; !exists {
		return
	}
	delete(byName, name)
	if len(byName) == 0 {
		delete(byHandler, handler)
	}
	if len(byHandler) == 0 {
		delete(t.global, eventType)
	}
	if t.listener != nil && t.bus != nil {
		t.globalSubCnt[eventType]--
		if t.globalSubCnt[eventType] <= 0 {
			delete(t.globalSubCnt, eventType)
			t.bus.UnSubscribeGlobal(eventType, t.listener)
		}
	}
}

func (t *Processor) BindServerHandler(eventType def.EventType, name string, handler inf.IEventHandler, callback inf.EventHandlerAny) {
	t.mu.Lock()
	defer t.mu.Unlock()
	byHandler := t.server[eventType]
	if byHandler == nil {
		byHandler = make(map[inf.IEventHandler]map[string]inf.EventHandlerAny)
		t.server[eventType] = byHandler
	}
	byName := byHandler[handler]
	if byName == nil {
		byName = make(map[string]inf.EventHandlerAny)
		byHandler[handler] = byName
	}
	_, existed := byName[name]
	byName[name] = callback

	if t.listener != nil && t.bus != nil && !existed {
		if t.serverSubCnt[eventType] == 0 {
			t.bus.SubscribeServer(eventType, t.listener)
		}
		t.serverSubCnt[eventType]++
	}
}

func (t *Processor) UnbindServerHandler(eventType def.EventType, name string, handler inf.IEventHandler) {
	t.mu.Lock()
	defer t.mu.Unlock()
	byHandler := t.server[eventType]
	if byHandler == nil {
		return
	}
	byName := byHandler[handler]
	if byName == nil {
		return
	}
	if _, exists := byName[name]; !exists {
		return
	}
	delete(byName, name)
	if len(byName) == 0 {
		delete(byHandler, handler)
	}
	if len(byHandler) == 0 {
		delete(t.server, eventType)
	}
	if t.listener != nil && t.bus != nil {
		t.serverSubCnt[eventType]--
		if t.serverSubCnt[eventType] <= 0 {
			delete(t.serverSubCnt, eventType)
			t.bus.UnSubscribeServer(eventType, t.listener)
		}
	}
}

func (t *Processor) BindSpecificHandler(eventType def.EventType, serviceUid string, name string, handler inf.IEventHandler, callback inf.EventHandlerAny) {
	t.mu.Lock()
	defer t.mu.Unlock()

	k := specificKey{eventType: eventType, serviceUid: serviceUid}
	byHandler := t.specific[k]
	if byHandler == nil {
		byHandler = make(map[inf.IEventHandler]map[string]inf.EventHandlerAny)
		t.specific[k] = byHandler
	}
	byName := byHandler[handler]
	if byName == nil {
		byName = make(map[string]inf.EventHandlerAny)
		byHandler[handler] = byName
	}
	_, existed := byName[name]
	byName[name] = callback

	if t.listener != nil && t.bus != nil && !existed {
		if t.specificSubCnt[k] == 0 {
			t.bus.SubscribeSpecific(eventType, serviceUid, t.listener)
		}
		t.specificSubCnt[k]++
	}
}

func (t *Processor) UnbindSpecificHandler(eventType def.EventType, serviceUid string, name string, handler inf.IEventHandler) {
	t.mu.Lock()
	defer t.mu.Unlock()

	k := specificKey{eventType: eventType, serviceUid: serviceUid}
	byHandler := t.specific[k]
	if byHandler == nil {
		return
	}
	byName := byHandler[handler]
	if byName == nil {
		return
	}
	if _, exists := byName[name]; !exists {
		return
	}
	delete(byName, name)
	if len(byName) == 0 {
		delete(byHandler, handler)
	}
	if len(byHandler) == 0 {
		delete(t.specific, k)
	}
	if t.listener != nil && t.bus != nil {
		t.specificSubCnt[k]--
		if t.specificSubCnt[k] <= 0 {
			delete(t.specificSubCnt, k)
			t.bus.UnSubscribeSpecific(eventType, serviceUid, t.listener)
		}
	}
}

// Trigger 本地同步触发
func (t *Processor) Trigger(ctx context.Context, eventType def.EventType, data any) {
	entries := t.snapshotLocal(eventType)
	for _, e := range entries {
		if err := t.safeExec(e, ctx, eventType, data); err != nil {
			if t.logger != nil {
				t.logger.WithContext(ctx).WithField("eventType", eventType).Errorf("trigger handler failed: %v", err)
			}
		}
	}
}

// EventHandler 由服务的 MailboxJobTypeEvent handler 调用
// 这里会把 *actor.Event 作为 data 传给已注册的集群事件回调（wrapper里可自动反序列化到具体类型）
func (t *Processor) EventHandler(ctx context.Context, ev *actor.Event) {
	if ev == nil {
		return
	}
	et := def.EventType(ev.GetType())

	entries := t.snapshotCluster(et, ev.GetServiceUid())
	if len(entries) == 0 {
		return
	}
	payload := ev.GetPayload()
	data, err := codec.DecodeFromAny(payload)
	if err != nil {
		if t.logger != nil {
			t.logger.WithContext(ctx).WithField("eventType", et).Errorf("unmarshal event payload failed: %v", err)
		}
		return
	}
	for _, e := range entries {
		if err := t.safeExec(e, ctx, et, data); err != nil {
			if t.logger != nil {
				t.logger.WithContext(ctx).WithField("eventType", et).Errorf("trigger handler failed: %v", err)
			}
		}
	}
}

func (t *Processor) PublishGlobal(ctx context.Context, eventType def.EventType, data proto.Message) error {
	t.mu.RLock()
	bus := t.bus
	t.mu.RUnlock()
	if bus == nil {
		return fmt.Errorf("event bus is nil")
	}
	return bus.PublishGlobal(ctx, eventType, data)
}

func (t *Processor) PublishServer(ctx context.Context, eventType def.EventType, data proto.Message) error {
	t.mu.RLock()
	listener := t.listener
	bus := t.bus
	t.mu.RUnlock()
	if bus == nil {
		return fmt.Errorf("event bus is nil")
	}
	if listener == nil {
		return bus.PublishServer(ctx, eventType, 0, data)
	}
	return bus.PublishServer(ctx, eventType, listener.GetPartition(), data)
}

func (t *Processor) PublishSpecific(ctx context.Context, eventType def.EventType, serviceUid string, data proto.Message) error {
	t.mu.RLock()
	bus := t.bus
	t.mu.RUnlock()
	if bus == nil {
		return fmt.Errorf("event bus is nil")
	}
	return bus.PublishSpecific(ctx, eventType, serviceUid, data)
}

func (t *Processor) snapshotLocal(eventType def.EventType) []callbackEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()
	byHandler := t.local[eventType]
	if len(byHandler) == 0 {
		return nil
	}
	out := make([]callbackEntry, 0)
	for _, byName := range byHandler {
		for name, cb := range byName {
			out = append(out, callbackEntry{name: name, cb: cb})
		}
	}
	return out
}

func (t *Processor) snapshotCluster(eventType def.EventType, targetServiceUid string) []callbackEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var out []callbackEntry
	for _, byName := range t.global[eventType] {
		for name, cb := range byName {
			out = append(out, callbackEntry{name: name, cb: cb})
		}
	}
	for _, byName := range t.server[eventType] {
		for name, cb := range byName {
			out = append(out, callbackEntry{name: name, cb: cb})
		}
	}
	if targetServiceUid != "" {
		k := specificKey{eventType: eventType, serviceUid: targetServiceUid}
		for _, byName := range t.specific[k] {
			for name, cb := range byName {
				out = append(out, callbackEntry{name: name, cb: cb})
			}
		}
	}
	return out
}

func (t *Processor) safeExec(entry callbackEntry, ctx context.Context, eventType def.EventType, data any) error {
	defer func() {
		if err := recover(); err != nil {
			if t.logger != nil {
				t.logger.Errorf("trigger handler panic: eventType=%d, name=%s, err=%v\nstack=%s", eventType, entry.name, err, string(debug.Stack()))
			}
		}
	}()
	return entry.cb(ctx, data)
}

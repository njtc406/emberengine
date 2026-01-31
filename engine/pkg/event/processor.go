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
	"github.com/njtc406/emberengine/engine/pkg/utils/codec"
	"google.golang.org/protobuf/proto"
)

var _ inf.IEventProcessor = (*Processor)(nil)

type triggerEntry struct {
	name     string
	callback inf.EventHandlerAny
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

	local    map[def.EventType]map[inf.IEventHandler]*triggerEntry
	global   map[def.EventType]map[inf.IEventHandler]*triggerEntry
	server   map[def.EventType]map[inf.IEventHandler]*triggerEntry
	specific map[specificKey]map[inf.IEventHandler]*triggerEntry

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

	t.local = make(map[def.EventType]map[inf.IEventHandler]*triggerEntry)
	t.global = make(map[def.EventType]map[inf.IEventHandler]*triggerEntry)
	t.server = make(map[def.EventType]map[inf.IEventHandler]*triggerEntry)
	t.specific = make(map[specificKey]map[inf.IEventHandler]*triggerEntry)

	t.globalSubCnt = make(map[def.EventType]int)
	t.serverSubCnt = make(map[def.EventType]int)
	t.specificSubCnt = make(map[specificKey]int)

	t.listener = listener
}

func (t *Processor) HasHandler(eventType def.EventType) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.local[eventType])+len(t.global[eventType])+len(t.server[eventType]) > 0
}

func (t *Processor) Clear() {
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

	t.local = make(map[def.EventType]map[inf.IEventHandler]*triggerEntry)
	t.global = make(map[def.EventType]map[inf.IEventHandler]*triggerEntry)
	t.server = make(map[def.EventType]map[inf.IEventHandler]*triggerEntry)
	t.specific = make(map[specificKey]map[inf.IEventHandler]*triggerEntry)

	t.globalSubCnt = make(map[def.EventType]int)
	t.serverSubCnt = make(map[def.EventType]int)
	t.specificSubCnt = make(map[specificKey]int)
}

func (t *Processor) BindHandler(eventType def.EventType, name string, handler inf.IEventHandler, callback inf.EventHandlerAny) {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry := &triggerEntry{name: name, callback: callback}
	if t.local[eventType] == nil {
		t.local[eventType] = make(map[inf.IEventHandler]*triggerEntry)
	}
	t.local[eventType][handler] = entry
}

func (t *Processor) UnbindHandler(eventType def.EventType, name string, handler inf.IEventHandler) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if m := t.local[eventType]; m != nil {
		delete(m, handler)
		if len(m) == 0 {
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

	entry := &triggerEntry{name: name, callback: callback}
	if t.global[eventType] == nil {
		t.global[eventType] = make(map[inf.IEventHandler]*triggerEntry)
	}
	t.global[eventType][handler] = entry

	if t.listener != nil {
		if t.globalSubCnt[eventType] == 0 {
			GetEventBus().SubscribeGlobal(eventType, t.listener)
		}
		t.globalSubCnt[eventType]++
	}
}

func (t *Processor) UnbindGlobalHandler(eventType def.EventType, name string, handler inf.IEventHandler) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if m := t.global[eventType]; m != nil {
		if _, exists := m[handler]; exists {
			delete(m, handler)
			if len(m) == 0 {
				delete(t.global, eventType)
			}
			if t.listener != nil {
				t.globalSubCnt[eventType]--
				if t.globalSubCnt[eventType] <= 0 {
					delete(t.globalSubCnt, eventType)
					GetEventBus().UnSubscribeGlobal(eventType, t.listener)
				}
			}
		}
	}
}

func (t *Processor) BindServerHandler(eventType def.EventType, name string, handler inf.IEventHandler, callback inf.EventHandlerAny) {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry := &triggerEntry{name: name, callback: callback}
	if t.server[eventType] == nil {
		t.server[eventType] = make(map[inf.IEventHandler]*triggerEntry)
	}
	t.server[eventType][handler] = entry

	if t.listener != nil {
		if t.serverSubCnt[eventType] == 0 {
			GetEventBus().SubscribeServer(eventType, t.listener)
		}
		t.serverSubCnt[eventType]++
	}
}

func (t *Processor) UnbindServerHandler(eventType def.EventType, name string, handler inf.IEventHandler) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if m := t.server[eventType]; m != nil {
		if _, exists := m[handler]; exists {
			delete(m, handler)
			if len(m) == 0 {
				delete(t.server, eventType)
			}
			if t.listener != nil {
				t.serverSubCnt[eventType]--
				if t.serverSubCnt[eventType] <= 0 {
					delete(t.serverSubCnt, eventType)
					GetEventBus().UnSubscribeServer(eventType, t.listener)
				}
			}
		}
	}
}

func (t *Processor) BindSpecificHandler(eventType def.EventType, serviceUid string, name string, handler inf.IEventHandler, callback inf.EventHandlerAny) {
	t.mu.Lock()
	defer t.mu.Unlock()

	k := specificKey{eventType: eventType, serviceUid: serviceUid}
	entry := &triggerEntry{name: name, callback: callback}
	if t.specific[k] == nil {
		t.specific[k] = make(map[inf.IEventHandler]*triggerEntry)
	}
	t.specific[k][handler] = entry

	if t.listener != nil {
		if t.specificSubCnt[k] == 0 {
			GetEventBus().SubscribeSpecific(eventType, serviceUid, t.listener)
		}
		t.specificSubCnt[k]++
	}
}

func (t *Processor) UnbindSpecificHandler(eventType def.EventType, serviceUid string, name string, handler inf.IEventHandler) {
	t.mu.Lock()
	defer t.mu.Unlock()

	k := specificKey{eventType: eventType, serviceUid: serviceUid}
	if m := t.specific[k]; m != nil {
		if _, exists := m[handler]; exists {
			delete(m, handler)
			if len(m) == 0 {
				delete(t.specific, k)
			}
			if t.listener != nil {
				t.specificSubCnt[k]--
				if t.specificSubCnt[k] <= 0 {
					delete(t.specificSubCnt, k)
					GetEventBus().UnSubscribeSpecific(eventType, serviceUid, t.listener)
				}
			}
		}
	}
}

// Processor 本地同步触发（不经过EventBus）
func (t *Processor) Trigger(ctx context.Context, eventType def.EventType, data any) {
	entries := t.snapshotLocal(eventType)
	for _, e := range entries {
		if err := t.safeExec(e, ctx, eventType, data); err != nil {
			log.SysLogger.WithContext(ctx).WithField("eventType", eventType).Errorf("trigger handler failed: %v", err)
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
		log.SysLogger.WithContext(ctx).WithField("eventType", et).Errorf("unmarshal event payload failed: %v", err)
		return
	}
	for _, e := range entries {
		if err := t.safeExec(e, ctx, et, data); err != nil {
			log.SysLogger.WithContext(ctx).WithField("eventType", et).Errorf("trigger handler failed: %v", err)
		}
	}
}

func (t *Processor) PublishGlobal(ctx context.Context, eventType def.EventType, data proto.Message) error {
	return GetEventBus().PublishGlobal(ctx, eventType, data)
}

func (t *Processor) PublishServer(ctx context.Context, eventType def.EventType, data proto.Message) error {
	t.mu.RLock()
	listener := t.listener
	t.mu.RUnlock()
	if listener == nil {
		return GetEventBus().PublishServer(ctx, eventType, 0, data)
	}
	return GetEventBus().PublishServer(ctx, eventType, listener.GetPartition(), data)
}

func (t *Processor) PublishSpecific(ctx context.Context, eventType def.EventType, serviceUid string, data proto.Message) error {
	return GetEventBus().PublishSpecific(ctx, eventType, serviceUid, data)
}

func (t *Processor) snapshotLocal(eventType def.EventType) []*triggerEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()
	m := t.local[eventType]
	if len(m) == 0 {
		return nil
	}
	out := make([]*triggerEntry, 0, len(m))
	for _, e := range m {
		out = append(out, e)
	}
	return out
}

func (t *Processor) snapshotCluster(eventType def.EventType, targetServiceUid string) []*triggerEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var out []*triggerEntry
	for _, e := range t.global[eventType] {
		out = append(out, e)
	}
	for _, e := range t.server[eventType] {
		out = append(out, e)
	}
	if targetServiceUid != "" {
		k := specificKey{eventType: eventType, serviceUid: targetServiceUid}
		for _, e := range t.specific[k] {
			out = append(out, e)
		}
	}
	return out
}

func (t *Processor) safeExec(entry *triggerEntry, ctx context.Context, eventType def.EventType, data any) error {
	defer func() {
		if err := recover(); err != nil {
			log.SysLogger.Errorf("trigger handler panic: eventType=%d, name=%s, err=%v", eventType, entry.name, err)
		}
	}()
	return entry.callback(ctx, data)
}

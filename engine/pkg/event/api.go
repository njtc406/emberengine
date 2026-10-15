package event

import (
	"context"
	"fmt"
	"reflect"

	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

type payloadGetter interface {
	GetPayload() *anypb.Any
}

func newProtoMessage[T proto.Message]() (T, bool) {
	var zero T
	t := reflect.TypeOf(zero)
	if t == nil {
		return zero, false
	}
	if t.Kind() != reflect.Ptr {
		return zero, false
	}
	v := reflect.New(t.Elem())
	msg, ok := v.Interface().(T)
	return msg, ok
}

// RegisterHandler 注册本地事件处理器
func RegisterHandler[T any](handler inf.IEventHandlerRegistrar, eventType def.EventType, name string, callback inf.EventHandler[T]) error {
	wrapper := func(ctx context.Context, data any) error {
		if typed, ok := data.(T); ok {
			return callback(ctx, typed)
		}
		var zero T
		return fmt.Errorf("data is not %T, type: %T", zero, data)
	}
	return handler.RegisterEvent(eventType, name, wrapper)
}

// RegisterGlobHandler 注册全局事件处理器（集群事件），从 *actor.Event.Payload 自动解码到具体 proto 类型 T。
func RegisterGlobHandler[T proto.Message](handler *Handler, eventType def.EventType, name string, callback inf.EventHandler[T]) error {
	wrapper := func(ctx context.Context, data any) error {
		pg, ok := data.(payloadGetter)
		if !ok {
			return fmt.Errorf("data is not payloadGetter, type: %T", data)
		}
		payload := pg.GetPayload()
		var msg T
		if payload != nil {
			msg, ok = newProtoMessage[T]()
			if !ok {
				return fmt.Errorf("newProtoMessage[T]() failed")
			}
			if err := payload.UnmarshalTo(msg); err != nil {
				return fmt.Errorf("unmarshal payload to %T failed: %w", msg, err)
			}
		}

		return callback(ctx, msg)
	}
	return handler.RegisterGlobalEvent(eventType, name, wrapper)
}

// RegisterServerHandler 注册服务器事件处理器（集群事件），从 *actor.Event.Payload 自动解码到具体 proto 类型 T。
func RegisterServerHandler[T proto.Message](handler *Handler, eventType def.EventType, name string, callback inf.EventHandler[T]) error {
	wrapper := func(ctx context.Context, data any) error {
		pg, ok := data.(payloadGetter)
		if !ok {
			return fmt.Errorf("data is not payloadGetter, type: %T", data)
		}
		payload := pg.GetPayload()
		var msg T
		if payload != nil {
			msg, ok = newProtoMessage[T]()
			if !ok {
				return fmt.Errorf("newProtoMessage[T]() failed")
			}
			if err := payload.UnmarshalTo(msg); err != nil {
				return fmt.Errorf("unmarshal payload to %T failed: %w", msg, err)
			}
			return callback(ctx, msg)
		}
		return callback(ctx, msg)
	}
	return handler.RegisterServerEvent(eventType, name, wrapper)
}

// RegisterSpecificHandler 注册特定服务事件处理器（集群事件），从 *actor.Event.Payload 自动解码到具体 proto 类型 T。
func RegisterSpecificHandler[T proto.Message](handler *Handler, eventType def.EventType, serviceUid string, name string, callback inf.EventHandler[T]) error {
	wrapper := func(ctx context.Context, data any) error {
		pg, ok := data.(payloadGetter)
		if !ok {
			return fmt.Errorf("data is not payloadGetter, type: %T", data)
		}
		payload := pg.GetPayload()
		var msg T
		if payload != nil {
			msg, ok = newProtoMessage[T]()
			if !ok {
				return fmt.Errorf("newProtoMessage[T]() failed")
			}
			if err := payload.UnmarshalTo(msg); err != nil {
				return fmt.Errorf("unmarshal payload to %T failed: %w", msg, err)
			}
		}
		return callback(ctx, msg)
	}
	return handler.RegisterSpecificEvent(eventType, serviceUid, name, wrapper)
}

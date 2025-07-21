// Package actor
// @Title  title
// @Description  desc
// @Author  yr  2025/4/10
// @Update  yr  2025/4/10
package actor

import (
	"context"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/utils/xcontext"
	"google.golang.org/protobuf/proto"
	"strconv"
	"time"
)

func (e *Event) GetType() int32 {
	return e.EventType
}

func (e *Event) GetDispatcherKey() string {
	return e.Data.Header[def.DefaultDispatcherKey]
}

func (e *Event) GetPriority() int32 {
	priority := e.Data.Header[def.DefaultPriorityKey]
	if priority != "" {
		priorityInt, err := strconv.Atoi(priority)
		if err == nil {
			return int32(priorityInt)
		}
	}

	return def.PriorityUser
}

func (e *Event) Marshal() ([]byte, error) {
	return proto.Marshal(e)
}

func (e *Event) Release() {}

func (e *Event) Ref() {

}

func (e *Event) UnRef() {

}

func (e *Event) IsRef() bool {
	return true
}

func (e *Event) Done() <-chan struct{} {
	return nil
}

func (e *Event) Deadline() (deadline time.Time, ok bool) {
	return
}

func (e *Event) Err() error {
	return nil
}

func (e *Event) Value(key interface{}) interface{} {
	return nil
}

func (e *Event) SetHeaders(headers map[string]any) {

}

func (e *Event) SetHeadersWithMap(headers map[string]string) {

}

func (e *Event) SetHeader(key string, value any) {

}
func (e *Event) GetHeader(key string) any {
	return e.Data.Header[key]
}
func (e *Event) GetHeaders() map[string]any {
	headers := make(map[string]any)
	for k, v := range e.Data.Header {
		headers[k] = v
	}
	return headers
}
func (e *Event) GetContext() context.Context {
	ctx := xcontext.New(nil)
	for k, v := range e.Data.Header {
		ctx.SetHeader(k, v)
	}
	return ctx
}
func (e *Event) GetTranceId() string {
	return e.Data.Header[def.DefaultTraceIdKey]
}

func (e *Event) ToHeaders() map[string]string {
	return e.Data.Header
}

// Package actor
// @Title  title
// @Description  desc
// @Author  yr  2025/4/10
// @Update  yr  2025/4/10
package actor

import (
	"context"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/utils/emberctx"
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

func (e *Event) SetHeaders(headers map[string]string) {

}
func (e *Event) SetHeader(key string, value any) {

}
func (e *Event) GetHeader(key string) string {
	return e.Data.Header[key]
}
func (e *Event) GetHeaders() map[string]string {
	return e.Data.Header
}
func (e *Event) GetContext() context.Context {
	ctx := emberctx.NewCtx(context.Background())
	return emberctx.WithHeader(ctx, e.Data.Header)
}
func (e *Event) GetTranceId() string {
	return e.Data.Header[def.DefaultTraceIdKey]
}

// Package actor
// @Title  title
// @Description  desc
// @Author  yr  2025/4/10
// @Update  yr  2025/4/10
package actor

import (
	"github.com/njtc406/emberengine/engine/pkg/def"
	"strconv"
)

func (e *Event) GetType() int32 {
	return e.EventType
}

func (e *Event) GetKey() string {
	return e.Data.Header[def.DefaultDispatcherKey]
}

func (e *Event) GetPriority() int32 {
	priority := e.Data.Header[def.DefaultPriorityKey]
	if priority == "" {
		return 0
	} else {
		priorityInt, err := strconv.Atoi(priority)
		if err != nil {
			return 0
		} else {
			return int32(priorityInt)
		}
	}
}

func (e *Event) Release() {}

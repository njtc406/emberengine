// Package actor
// @Title  title
// @Description  desc
// @Author  yr  2025/4/10
// @Update  yr  2025/4/10
package actor

import "strconv"

func (e *Event) GetType() int32 {
	return e.EventType
}

func (e *Event) GetKey() string {
	return e.Data.Header["DispatchKey"]
}

func (e *Event) GetPriority() int32 {
	priority := e.Data.Header["Priority"]
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

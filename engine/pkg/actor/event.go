// Package actor
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2026/1/30 01:38
// 最后更新:  yr  2026/1/30 01:38
package actor

import (
	"github.com/njtc406/emberengine/engine/pkg/def"
)

func (e *Event) GetData() any {
	return e.Payload
}

func (e *Event) GetEventType() def.EventType {
	return def.EventType(e.Type)
}

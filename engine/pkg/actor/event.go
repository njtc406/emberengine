// Package actor
// 提供 actor 协议消息的运行时辅助方法。
// 作者:  yr  2026/1/30 01:38
// 最后更新:  yr  2026/1/30 01:38
package actor

import (
	"github.com/njtc406/emberengine/engine/pkg/def"
)

func (e *Event) GetEventType() def.EventType {
	return def.EventType(e.Type)
}

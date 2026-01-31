// Package interfaces
// @Title  title
// @Description  desc
// @Author  pc  2024/11/5
// @Update  pc  2024/11/5
package interfaces

import (
	"github.com/njtc406/emberengine/engine/pkg/def"
)

type IEvent interface {
	GetEventType() def.EventType
}

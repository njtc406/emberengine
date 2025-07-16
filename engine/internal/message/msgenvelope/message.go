// Package msgenvelope
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/16 0016 2:19
// 最后更新:  yr  2025/7/16 0016 2:19
package msgenvelope

import (
	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
)

var msgPool = pool.NewPrePPool(1024, func() *actor.Message {
	return &actor.Message{}
})

func NewMessage() *actor.Message {
	return msgPool.Get()
}

func ReleaseMessage(msg *actor.Message) {
	msgPool.Put(msg)
}

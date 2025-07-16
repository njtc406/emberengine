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

var msgPool = pool.NewPool(make(chan interface{}, 1000), func() interface{} {
	return &actor.Message{}
})

func NewMessage() *actor.Message {
	return msgPool.Get().(*actor.Message)
}

func ReleaseMessage(msg *actor.Message) {
	msgPool.Put(msg)
}

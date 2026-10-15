// Package msgenvelope
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/16 0016 2:19
// 最后更新:  yr  2025/7/16 0016 2:19
package msgenvelope

import (
	"sync"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
)

// TODO 需要做成配置
var msgPool pool.IPool[*actor.Message]
var msgInitOnce sync.Once

func getMsgPool() pool.IPool[*actor.Message] {
	msgInitOnce.Do(func() {
		msgPool = pool.NewPerPPoolWrapper(
			1024,
			func() *actor.Message {
				return &actor.Message{}
			},
			func() pool.IStatsRecorder {
				if isDebug() {
					return pool.NewStatsRecorder("rpcMsgPool-syncPool")
				} else {
					return pool.NewNoStatsRecorder()
				}
			}(),
			pool.WithPReset(func(msg *actor.Message) {
				msg.Reset()
			}),
		)
	})
	return msgPool
}

func NewMessage() *actor.Message {
	return getMsgPool().Get()
}

func ReleaseMessage(msg *actor.Message) {
	getMsgPool().Put(msg)
}

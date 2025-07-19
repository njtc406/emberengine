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

// TODO 需要做成配置
var msgPool = pool.NewPerPPoolWrapper(
	1024, // 自行根据并发量设置,压测时10w个请求,并发数为500,设置为1024已经满足要求
	func() *actor.Message {
		return &actor.Message{}
	},
	pool.NewStatsRecorder("rpcMsgPool-perpPool"),
	pool.WithPReset(func(msg *actor.Message) {
		msg.Reset()
	}),
)

func NewMessage() *actor.Message {
	return msgPool.Get()
}

func ReleaseMessage(msg *actor.Message) {
	msgPool.Put(msg)
}

func GetMsgPoolStats() *pool.Stats {
	return msgPool.Stats()
}

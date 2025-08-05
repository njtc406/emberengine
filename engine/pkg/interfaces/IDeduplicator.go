// Package interfaces
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/8/6 0006 0:53
// 最后更新:  yr  2025/8/6 0006 0:53
package interfaces

type IDeDuplicator interface {
	Seen(serviceUid string, id uint64) bool
	MarkDone(serviceUid string, id uint64)
}

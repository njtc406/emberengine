// Package interfaces
// @Title  并发回调接口
// @Description  用于并发任务完成后的回调处理
// @Author  yr  2026/1/4
// @Update  yr  2026/1/4
package interfaces

import "context"

// IConcurrentCallback 并发回调接口
//
// 实现此接口的类型可以通过 mailbox 投递并在目标服务的 goroutine 中执行回调。
// 典型使用场景：
//   - AsyncCall 的回调处理（CallState）
//   - 异步任务完成通知
//
// 注意：回调将在目标服务的 mailbox worker 中执行，需保证线程安全。
type IConcurrentCallback interface {
	INamed
	DoCallback(ctx context.Context)
}

// Package timingwheel
// 临时全局兼容（Node 自包含改造过渡期）。
// 后续 Phase 中将删除本文件。
package timingwheel

// ── 临时全局兼容 ──
// defaultTW 由 Node.Start 设置。
// Deprecated: 后续 Phase 删除。
var defaultTW *TimingWheel

// SetDefaultTimingWheel 由 Node.Start 调用。
// Deprecated: 后续 Phase 删除。
func SetDefaultTimingWheel(tw *TimingWheel) { defaultTW = tw }

// GetTimingWheel 返回默认全局时间轮（临时兼容）。
// Deprecated: 请通过 NodeContext.TimingWheel() 获取。
func GetTimingWheel() *TimingWheel {
	return defaultTW
}

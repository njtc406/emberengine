package dedup

import inf "github.com/njtc406/emberengine/engine/pkg/interfaces"

// ── 临时全局兼容 ──
// defaultDeDuplicator 由 Node.Start 设置。
// Deprecated: 后续 Phase 中删除。
var defaultDeDuplicator inf.IDeDuplicator

// SetDefaultDeDuplicator 由 Node.Start 调用。
// Deprecated: 后续 Phase 删除。
func SetDefaultDeDuplicator(d inf.IDeDuplicator) { defaultDeDuplicator = d }

// GetDeDuplicator 返回默认全局去重器（临时兼容）。
// Deprecated: 请通过 NodeContext.DeDuplicator() 获取。
func GetDeDuplicator() inf.IDeDuplicator {
	return defaultDeDuplicator
}

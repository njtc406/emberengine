package core

import inf "github.com/njtc406/emberengine/engine/pkg/interfaces"

// GetModule 按 moduleId 查找并转换为目标模块接口/类型。
// 返回 false 表示模块不存在或类型不匹配。
func GetModule[T any](hierarchy inf.IModuleHierarchy, moduleID uint32) (T, bool) {
	var zero T
	if hierarchy == nil {
		return zero, false
	}
	module := hierarchy.GetModule(moduleID)
	if module == nil {
		return zero, false
	}
	target, ok := module.(T)
	if !ok {
		return zero, false
	}
	return target, true
}

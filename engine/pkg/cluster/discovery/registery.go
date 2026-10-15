// Package discovery
// @Title  title
// @Description  desc
// @Author  yr  2025/5/6
// @Update  yr  2025/5/6
package discovery

import (
	"sync"

	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

// 服务发现工厂注册中心
// discoveryFactory 存储的是无状态的工厂函数（而非实例），
// 属于「只读注册表」类型，多 Node 共享同一份工厂函数不会冲突。
var (
	discoveryFactory sync.Map
)

// Register 注册服务发现工厂函数。
// 每次 CreateDiscovery 调用都会通过工厂函数创建全新实例。
func Register(name string, creator func() inf.IDiscovery) {
	discoveryFactory.Store(name, creator)
}

// CreateDiscovery 创建指定类型的服务发现实例（每次调用创建新实例）。
func CreateDiscovery(name string) inf.IDiscovery {
	v, ok := discoveryFactory.Load(name)
	if !ok {
		return nil
	}
	return v.(func() inf.IDiscovery)()
}

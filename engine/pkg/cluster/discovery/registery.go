// Package discovery
// @Title  title
// @Description  desc
// @Author  yr  2025/5/6
// @Update  yr  2025/5/6
package discovery

import (
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"sync"
)

// 服务发现注册中心
var (
	discoveryRegistry sync.Map
)

// Register 注册服务发现实现
func Register(name string, discovery inf.IDiscovery) {
	discoveryRegistry.Store(name, discovery)
}

// CreateDiscovery 创建指定类型的服务发现实例
func CreateDiscovery(name string) inf.IDiscovery {
	v, ok := discoveryRegistry.Load(name)
	if !ok {
		return nil
	}
	return v.(inf.IDiscovery)
}

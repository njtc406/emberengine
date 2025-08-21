// Package router
// 模块名: 模块名
// 功能描述: 用于玩家路由查找
// 作者:  yr  2025/8/22 0022 0:48
// 最后更新:  yr  2025/8/22 0022 0:48
package router

import (
	"github.com/njtc406/emberengine/engine/pkg/core"
	"github.com/patrickmn/go-cache"
)

type Router struct {
	core.Module

	routerMap *cache.Cache // 玩家路由
}

func (r *Router) OnInit() error {
	r.routerMap = cache.New(cache.NoExpiration, cache.NoExpiration)
	return nil
}

func (r *Router) OnRelease() error {
	// 移除所有路由
	r.routerMap.Flush()
	return nil
}

// Package router
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/8/22 0022 0:51
// 最后更新:  yr  2025/8/22 0022 0:51
package router

import (
	"github.com/njtc406/emberengine/engine/pkg/core/rpc"
	"github.com/njtc406/emberengine/engine/pkg/def"
)

// TODO 看要不要使用事务来存, 看最后的数据一致性要求, 如果允许短暂不一致就不需要

func (r *Router) ApiGetUserRouter(rid string, method string) (string, error) {
	var url string
	v, ok := r.routerMap.Get(rid + method)
	if !ok {
		// 从缓存中获取
		if err := r.Select(rpc.WithServiceName("DBService"), rpc.WithServerId(r.GetService().GetServerId())).
			Call(nil, "ApiRedisGetString", "prefix.router"+rid+"."+method, &url); err != nil {
			r.GetLogger().Errorf("get user router failed, err:%v", err)
		}
		if url == "" {
			return "", def.ErrCantFoundRouter
		}
		r.routerMap.SetDefault(rid+method, url)
	} else {
		url = v.(string)
	}
	return url, nil
}

func (r *Router) ApiSetUserRouter(rid string, method string, url string) error {
	if err := r.Select(rpc.WithServiceName("DBService"), rpc.WithServerId(r.GetService().GetServerId())).
		Call(nil, "ApiRedisSetString", "prefix.router"+rid+"."+method, url); err != nil {
		r.GetLogger().Errorf("set user router failed, err:%v", err)
		return err
	}
	r.routerMap.SetDefault(rid+method, url)
	return nil
}

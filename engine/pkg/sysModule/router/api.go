// Package router
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/8/22 0022 0:51
// 最后更新:  yr  2025/8/22 0022 0:51
package router

import (
	"errors"

	"github.com/njtc406/emberengine/engine/pkg/core/rpc"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/utils/xcontext"
	"github.com/redis/go-redis/v9"
)

// TODO 看要不要使用事务来存, 看最后的数据一致性要求, 如果允许短暂不一致就不需要

func buildKey(rid string, method string) string {
	return "prefix.router" + rid + "." + method
}

func (r *Router) ApiGetUserRouter(rid string, method string) (string, error) {
	var url string
	v, ok := r.routerMap.Get(rid + method)
	if !ok {
		// TODO 缓存未命中,记录统计
		// 从缓存中获取
		key := buildKey(rid, method)
		if err := r.Select(
			rpc.WithName("DBService"),
			rpc.WithPartition(r.GetService().GetPartition()),
		).Call(
			nil,
			"ApiRedisGetString",
			key,
			&url,
		); err != nil {
			if errors.Is(err, redis.ErrClosed) {
				return "", def.RouterNotFound
			}
			r.GetLogger().Errorf("get user router failed, err:%v", err)
		}
		if url == "" {
			return "", def.RouterNotFound
		}
		r.routerMap.SetDefault(key, url)
	} else {
		url = v.(string)
	}
	return url, nil
}

func (r *Router) ApiSetUserRouter(rid string, method string, url string) error {
	key := buildKey(rid, method)
	if err := r.Select(rpc.WithName("DBService"), rpc.WithPartition(r.GetService().GetPartition())).
		Call(nil, "ApiRedisSetString", key, url); err != nil {
		r.GetLogger().Errorf("set user router failed, err:%v", err)
		return err
	}
	r.routerMap.SetDefault(key, url)
	// TODO 通知所有router模块缓存更新
	return nil
}

func (r *Router) ApiCleanRouter(rid string) error {
	ctx := xcontext.New(nil)
	if err := r.Select(
		rpc.WithName("DBService"),
		rpc.WithPartition(r.GetService().GetPartition()),
	).Call(nil, "ApiRedisDel", "prefix.router"+rid, nil); err != nil {
		r.GetLogger().WithContext(ctx).WithFields(map[string]interface{}{
			"rid": rid,
		}).Errorf("clean user router failed, err:%v", err)
		return err
	}
	r.routerMap.Delete(rid)

	// TODO 通知所有router模块缓存更新
	return nil
}

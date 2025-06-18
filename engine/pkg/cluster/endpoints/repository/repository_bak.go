// Package repository
// @Title  服务存储器
// @Description  用于存放所有服务的注册信息,包括本地和远程的服务信息
// @Author  yr  2024/11/7
// @Update  yr  2024/11/7
package repository

import (
	"time"

	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

// TODO 存储改造,使用go-memdb内存数据库来存储所有的pid数据
// 同时他还提供了索引功能,可以快速查询指定服务

type tmpInfo struct {
	dispatcher inf.IRpcDispatcher
	latest     time.Time
}

//type Repository struct {
//	mapPID         *sync.Map // 服务 [serviceUid]interfaces.IRpcDispatcher
//	tmpMapPid      *sync.Map // 临时服务 [serviceUid]tmpInfo
//	tmpMapStrategy func(*tmpInfo) bool
//
//	ticker *time.Ticker
//
//	// 快速查询表
//	mapNodeLock          *shardedlock.ShardedRWLock
//	mapSvcBySNameAndSUid map[string]map[string]struct{}            // [serviceName]map[serviceUid]struct{}
//	mapSvcBySTpAndSName  map[string]map[string]map[string]struct{} // [serviceType]map[serviceName]map[serviceUid]struct{}
//	// TODO 之后可以加入tag索引表,每种service自定义自己的tag,这样可以更高效的查询指定服务
//}
//
//func NewRepository() *Repository {
//	return &Repository{
//		mapPID:               new(sync.Map),
//		tmpMapPid:            new(sync.Map),
//		ticker:               time.NewTicker(time.Second * 10),
//		mapNodeLock:          shardedlock.NewShardedRWLock(64), // 之后改为配置表
//		mapSvcBySNameAndSUid: make(map[string]map[string]struct{}),
//		mapSvcBySTpAndSName:  make(map[string]map[string]map[string]struct{}),
//	}
//}
//
//func (r *Repository) Start() {
//	r.tick()
//}
//
//func (r *Repository) Stop() {
//	r.ticker.Stop()
//	// 关闭所有连接
//	r.mapPID.Range(func(key, value any) bool {
//		if client, ok := value.(inf.IRpcDispatcher); ok {
//			client.Close()
//		}
//		return true
//	})
//}
//
//func (r *Repository) tick() {
//	go func() {
//		defer func() {
//			// 退出时关闭所有临时连接
//			r.tmpMapPid.Range(func(key, value any) bool {
//				if client, ok := value.(inf.IRpcDispatcher); ok {
//					client.Close()
//				}
//				return true
//			})
//		}()
//		for {
//			select {
//			case _, ok := <-r.ticker.C:
//				if !ok {
//					return
//				}
//				// TODO 这里考虑分批进行清理,防止出现热点问题
//				r.tmpMapPid.Range(func(key, value any) bool {
//					if tmp, ok := value.(*tmpInfo); ok {
//						if r.tmpMapStrategy == nil {
//							if r.defaultStrategy(tmp) {
//								r.tmpMapPid.Delete(key)
//							}
//						} else {
//							if r.tmpMapStrategy(tmp) {
//								r.tmpMapPid.Delete(key)
//							}
//						}
//					}
//					return true
//				})
//			}
//		}
//	}()
//}
//
//func (r *Repository) SetTmpMapStrategy(strategy func(*tmpInfo) bool) {
//	r.tmpMapStrategy = strategy
//}
//
//func (r *Repository) defaultStrategy(tmp *tmpInfo) bool {
//	// 5分钟未更新则删除
//	if timelib.Now().Sub(tmp.latest) > time.Minute*5 {
//		return true
//	}
//
//	return false
//}
//
//func (r *Repository) AddTmp(dispatcher inf.IRpcDispatcher) inf.IRpcDispatcher {
//	tmp := &tmpInfo{
//		dispatcher: dispatcher,
//		latest:     timelib.Now(),
//	}
//	r.tmpMapPid.Store(dispatcher.GetPid().GetServiceUid(), tmp)
//	//log.SysLogger.Infof("add tmp service: %s", dispatcher.GetPid().GetServiceUid())
//	return dispatcher
//}
//
//func (r *Repository) Add(dispatcher inf.IRpcDispatcher) {
//	pid := dispatcher.GetPid()
//	serviceUid := pid.GetServiceUid()
//	oldClient, ok := r.mapPID.LoadOrStore(serviceUid, dispatcher)
//	if ok {
//		//log.SysLogger.Debugf("service already exists: %s", dispatcher.GetPid().GetServiceUid())
//		oldClient.(inf.IRpcDispatcher).Close()                          // 旧的关闭
//		r.mapPID.Store(dispatcher.GetPid().GetServiceUid(), dispatcher) // 更新
//		return
//	}
//
//	r.mapNodeLock.Lock(serviceUid)
//	defer r.mapNodeLock.Unlock(serviceUid)
//
//	serviceType := pid.GetServiceType()
//	serviceName := pid.GetName()
//
//	nameMap, ok := r.mapSvcBySNameAndSUid[serviceName]
//	if !ok {
//		r.mapSvcBySNameAndSUid[serviceName] = make(map[string]struct{})
//		nameMap = r.mapSvcBySNameAndSUid[serviceName]
//	}
//
//	_, ok = nameMap[serviceUid]
//	if !ok {
//		nameMap[serviceUid] = struct{}{}
//	}
//
//	nodeNameUidMap, ok := r.mapSvcBySTpAndSName[serviceType]
//	if !ok {
//		r.mapSvcBySTpAndSName[serviceType] = make(map[string]map[string]struct{})
//		nodeNameUidMap = r.mapSvcBySTpAndSName[serviceType]
//	}
//
//	nameUidMap, ok := nodeNameUidMap[serviceName]
//	if !ok {
//		nodeNameUidMap[serviceName] = make(map[string]struct{})
//		nameUidMap = nodeNameUidMap[serviceName]
//	}
//
//	_, ok = nameUidMap[serviceUid]
//	if !ok {
//		nameUidMap[serviceUid] = struct{}{}
//	}
//}
//
//func (r *Repository) Remove(key string) {
//	ret, ok := r.mapPID.LoadAndDelete(key)
//	if !ok {
//		return
//	}
//	client := ret.(inf.IRpcDispatcher)
//	pid := client.GetPid()
//	client.Close()
//
//	r.mapNodeLock.Lock(key)
//	defer r.mapNodeLock.Unlock(key)
//	serviceName := pid.GetName()
//	serviceUid := pid.GetServiceUid()
//	serviceType := pid.GetServiceType()
//
//	nameMap, ok := r.mapSvcBySNameAndSUid[serviceName]
//	if ok {
//		delete(nameMap, serviceUid)
//		if len(nameMap) == 0 {
//			delete(r.mapSvcBySNameAndSUid, serviceName)
//		}
//	} else {
//		return
//	}
//
//	nodeNameUidMap, ok := r.mapSvcBySTpAndSName[serviceType]
//	if ok {
//		nameUidMap, ok := nodeNameUidMap[serviceName]
//		if ok {
//			delete(nameUidMap, serviceUid)
//			if len(nameUidMap) == 0 {
//				delete(nodeNameUidMap, serviceName)
//			}
//		}
//		if len(nodeNameUidMap) == 0 {
//			delete(r.mapSvcBySTpAndSName, serviceType)
//		}
//	}
//}

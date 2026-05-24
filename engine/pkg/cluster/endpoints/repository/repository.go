// Package repository
// @Title  服务存储器
// @Description  用于存放所有服务的注册信息,包括本地和远程的服务信息
// @Author  yr  2024/11/7
// @Update  yr  2024/11/7
package repository

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/rpc/message/msgbus"
	"github.com/njtc406/emberengine/engine/pkg/utils/shardedlock"
	"github.com/njtc406/emberengine/engine/pkg/utils/timelib"
)

// TODO 存储改造,使用go-memdb内存数据库来存储所有的pid数据
// 同时他还提供了索引功能,可以快速查询指定服务

const indexLockKey = "repository-index"

type tmpInfo struct {
	dispatcher inf.IRpcDispatcher
	latest     atomic.Int64
}

func newTmpInfo(dispatcher inf.IRpcDispatcher) *tmpInfo {
	info := &tmpInfo{dispatcher: dispatcher}
	info.touch()
	return info
}

func (info *tmpInfo) touch() {
	if info == nil {
		return
	}
	info.latest.Store(timelib.Now().UnixNano())
}

func (info *tmpInfo) lastActive() time.Time {
	if info == nil {
		return time.Time{}
	}
	latest := info.latest.Load()
	if latest == 0 {
		return time.Time{}
	}
	return time.Unix(0, latest)
}

func (info *tmpInfo) getDispatcher() inf.IRpcDispatcher {
	if info == nil {
		return nil
	}
	return info.dispatcher
}

func (info *tmpInfo) close() {
	if dispatcher := info.getDispatcher(); dispatcher != nil {
		dispatcher.Close()
	}
}

type serviceInfo struct {
	mu         sync.RWMutex
	dispatcher inf.IRpcDispatcher
	status     atomic.Int32
	visibility atomic.Int32
}

func newServiceInfo(dispatcher inf.IRpcDispatcher, status int32, visibility def.ServiceVisibility) *serviceInfo {
	info := &serviceInfo{dispatcher: dispatcher}
	info.status.Store(status)
	info.visibility.Store(int32(visibility))
	return info
}

func (info *serviceInfo) getDispatcher() inf.IRpcDispatcher {
	if info == nil {
		return nil
	}
	info.mu.RLock()
	defer info.mu.RUnlock()
	return info.dispatcher
}

func (info *serviceInfo) updateDispatcher(dispatcher inf.IRpcDispatcher) inf.IRpcDispatcher {
	if info == nil {
		return dispatcher
	}
	info.mu.Lock()
	defer info.mu.Unlock()
	old := info.dispatcher
	info.dispatcher = dispatcher
	return old
}

func (info *serviceInfo) syncPID(pid *actor.PID) {
	if info == nil || pid == nil {
		return
	}
	info.mu.RLock()
	dispatcher := info.dispatcher
	info.mu.RUnlock()
	if dispatcher == nil || dispatcher.GetPid() == nil {
		return
	}
	dispatcher.GetPid().SetMaster(pid.IsMasterNode())
}

type Repository struct {
	busFactory     *msgbus.MessageBusFactory
	keyMap         sync.Map
	mapPID         sync.Map     // 服务 [serviceUid]*serviceInfo
	tmpMapPid      sync.Map     // 临时服务 [serviceUid]tmpInfo
	tmpMapStrategy atomic.Value // stores func(*tmpInfo) bool

	ticker  *time.Ticker
	stopCh  chan struct{}
	runMu   sync.Mutex
	running bool
	stopped bool

	// 快速查询表
	mapNodeLock          *shardedlock.ShardedRWLock
	mapSvcBySNameAndSUid map[string]map[string]struct{}            // [serviceName]map[serviceUid]struct{}
	mapSvcBySTpAndSName  map[string]map[string]map[string]struct{} // [serviceType]map[serviceName]map[serviceUid]struct{}
	// TODO 之后可以加入tag索引表,每种service自定义自己的tag,这样可以更高效的查询指定服务
}

func NewRepository(busFactory *msgbus.MessageBusFactory) *Repository {
	return &Repository{
		busFactory:           busFactory,
		ticker:               time.NewTicker(time.Second * 10),
		stopCh:               make(chan struct{}),
		mapNodeLock:          shardedlock.NewShardedRWLock(64), // 之后改为配置表
		mapSvcBySNameAndSUid: make(map[string]map[string]struct{}),
		mapSvcBySTpAndSName:  make(map[string]map[string]map[string]struct{}),
	}
}

func (r *Repository) Start() {
	r.runMu.Lock()
	defer r.runMu.Unlock()
	if r.running || r.stopped {
		return
	}
	r.running = true
	r.tick()
}

func (r *Repository) Stop() {
	r.runMu.Lock()
	if !r.stopped {
		r.ticker.Stop()
		close(r.stopCh)
		r.stopped = true
		r.running = false
	}
	r.runMu.Unlock()
	// 关闭所有连接
	r.mapPID.Range(func(key, value any) bool {
		if info, ok := value.(*serviceInfo); ok {
			if dispatcher := info.getDispatcher(); dispatcher != nil {
				dispatcher.Close()
			}
		}
		return true
	})
	r.tmpMapPid.Range(func(key, value any) bool {
		if tmp, ok := value.(*tmpInfo); ok {
			if r.tmpMapPid.CompareAndDelete(key, tmp) {
				tmp.close()
			}
		}
		return true
	})
}

func (r *Repository) tick() {
	go func() {
		defer func() {
			// 退出时关闭所有临时连接
			r.tmpMapPid.Range(func(key, value any) bool {
				if tmp, ok := value.(*tmpInfo); ok {
					if r.tmpMapPid.CompareAndDelete(key, tmp) {
						tmp.close()
					}
				}
				return true
			})
		}()
		for {
			select {
			case <-r.stopCh:
				return
			case _, ok := <-r.ticker.C:
				if !ok {
					return
				}
				// TODO 这里考虑分批进行清理,防止出现热点问题
				r.tmpMapPid.Range(func(key, value any) bool {
					if tmp, ok := value.(*tmpInfo); ok {
						shouldDelete := false
						if strategy, ok := r.tmpMapStrategy.Load().(func(*tmpInfo) bool); ok && strategy != nil {
							shouldDelete = strategy(tmp)
						} else {
							shouldDelete = r.defaultStrategy(tmp)
						}
						if shouldDelete && r.tmpMapPid.CompareAndDelete(key, tmp) {
							tmp.close()
						}
					}
					return true
				})
			}
		}
	}()
}

func (r *Repository) SetTmpMapStrategy(strategy func(*tmpInfo) bool) {
	r.tmpMapStrategy.Store(strategy)
}

func (r *Repository) defaultStrategy(tmp *tmpInfo) bool {
	// 5分钟未更新则删除
	if timelib.Now().Sub(tmp.lastActive()) > time.Minute*5 {
		return true
	}

	return false
}

func (r *Repository) AddTmp(dispatcher inf.IRpcDispatcher) inf.IRpcDispatcher {
	if dispatcher == nil || dispatcher.GetPid() == nil {
		return dispatcher
	}
	serviceUid := dispatcher.GetPid().GetServiceUid()
	tmp := newTmpInfo(dispatcher)
	oldValue, loaded := r.tmpMapPid.LoadOrStore(serviceUid, tmp)
	if !loaded {
		return dispatcher
	}
	old, ok := oldValue.(*tmpInfo)
	if !ok {
		r.tmpMapPid.Store(serviceUid, tmp)
		return dispatcher
	}
	old.touch()
	oldDispatcher := old.getDispatcher()
	if oldDispatcher != nil {
		if oldDispatcher != dispatcher {
			dispatcher.Close()
		}
		return oldDispatcher
	}
	if r.tmpMapPid.CompareAndSwap(serviceUid, old, tmp) {
		return dispatcher
	}
	dispatcher.Close()
	return r.SelectByServiceUid(serviceUid)
}

func (r *Repository) Add(key string, dispatcher inf.IRpcDispatcher) {
	r.AddWithMeta(key, dispatcher, def.SvcStatusReady, def.ServiceVisibilityCluster)
}

func (r *Repository) AddWithMeta(key string, dispatcher inf.IRpcDispatcher, status int32, visibility def.ServiceVisibility) {
	pid := dispatcher.GetPid()
	serviceUid := pid.GetServiceUid()
	if key == "" {
		key = serviceUid
	}
	r.keyMap.Store(key, serviceUid)
	info := newServiceInfo(dispatcher, status, visibility)
	oldInfo, ok := r.mapPID.LoadOrStore(serviceUid, info)
	if ok {
		if old, ok := oldInfo.(*serviceInfo); ok {
			oldDispatcher := old.getDispatcher()
			if oldDispatcher != nil {
				old.syncPID(pid)
				old.visibility.Store(int32(visibility))
				old.status.Store(status)
				if dispatcher != nil && dispatcher != oldDispatcher {
					dispatcher.Close()
				}
				return
			}
			old.updateDispatcher(dispatcher)
			old.visibility.Store(int32(visibility))
			old.status.Store(status)
			return
		}
		oldValue, loaded := r.mapPID.LoadAndDelete(serviceUid)
		if loaded {
			if old, ok := oldValue.(*serviceInfo); ok {
				if oldDispatcher := old.getDispatcher(); oldDispatcher != nil {
					oldDispatcher.Close()
				}
			}
		}
		r.mapPID.Store(serviceUid, info)
		r.mapNodeLock.Lock(indexLockKey)
		defer r.mapNodeLock.Unlock(indexLockKey)
		r.indexAdd(pid)
		return
	}

	r.mapNodeLock.Lock(indexLockKey)
	defer r.mapNodeLock.Unlock(indexLockKey)

	r.indexAdd(pid)
}

func (r *Repository) UpdateStatus(serviceUid string, status int32) bool {
	value, ok := r.mapPID.Load(serviceUid)
	if !ok {
		return false
	}
	info, ok := value.(*serviceInfo)
	if !ok {
		return false
	}
	info.status.Store(status)
	return true
}

func (r *Repository) UpdateVisibility(serviceUid string, visibility def.ServiceVisibility) bool {
	value, ok := r.mapPID.Load(serviceUid)
	if !ok {
		return false
	}
	info, ok := value.(*serviceInfo)
	if !ok {
		return false
	}
	info.visibility.Store(int32(visibility))
	return true
}

func (r *Repository) IsSelectable(serviceUid string) bool {
	value, ok := r.mapPID.Load(serviceUid)
	if !ok {
		return false
	}
	info, ok := value.(*serviceInfo)
	return ok && r.isSelectableInfo(info)
}

func (r *Repository) IsRemoteCallable(serviceUid string) bool {
	value, ok := r.mapPID.Load(serviceUid)
	if !ok {
		return false
	}
	info, ok := value.(*serviceInfo)
	if !ok || info == nil {
		return false
	}
	visibility := def.ServiceVisibility(info.visibility.Load())
	return visibility == def.ServiceVisibilityCluster || visibility == def.ServiceVisibilityNode
}

func (r *Repository) isSelectableInfo(info *serviceInfo) bool {
	return info != nil && info.getDispatcher() != nil && info.status.Load() == def.SvcStatusReady
}

// indexAdd 维护按 serviceName / serviceType 的本地索引
func (r *Repository) indexAdd(pid *actor.PID) {
	serviceUid := pid.GetServiceUid()
	serviceType := pid.GetServiceType()
	serviceName := pid.GetName()

	nameMap, ok := r.mapSvcBySNameAndSUid[serviceName]
	if !ok {
		nameMap = make(map[string]struct{})
		r.mapSvcBySNameAndSUid[serviceName] = nameMap
	}
	nameMap[serviceUid] = struct{}{}

	nodeNameUidMap, ok := r.mapSvcBySTpAndSName[serviceType]
	if !ok {
		nodeNameUidMap = make(map[string]map[string]struct{})
		r.mapSvcBySTpAndSName[serviceType] = nodeNameUidMap
	}

	nameUidMap, ok := nodeNameUidMap[serviceName]
	if !ok {
		nameUidMap = make(map[string]struct{})
		nodeNameUidMap[serviceName] = nameUidMap
	}
	nameUidMap[serviceUid] = struct{}{}
}

func (r *Repository) Remove(key string) {
	if key == "" {
		return
	}
	val, ok := r.keyMap.LoadAndDelete(key)
	if !ok {
		return
	}
	serviceUid := val.(string)
	ret, ok := r.mapPID.LoadAndDelete(serviceUid)
	if !ok {
		return
	}
	info := ret.(*serviceInfo)
	client := info.getDispatcher()
	if client == nil {
		return
	}
	pid := client.GetPid()
	client.Close()

	r.mapNodeLock.Lock(indexLockKey)
	defer r.mapNodeLock.Unlock(indexLockKey)

	r.indexRemove(pid)
}

// indexRemove 从本地索引中移除指定 pid 相关条目
func (r *Repository) indexRemove(pid *actor.PID) {
	serviceUid := pid.GetServiceUid()
	serviceType := pid.GetServiceType()
	serviceName := pid.GetName()

	nameMap, ok := r.mapSvcBySNameAndSUid[serviceName]
	if ok {
		delete(nameMap, serviceUid)
		if len(nameMap) == 0 {
			delete(r.mapSvcBySNameAndSUid, serviceName)
		}
	} else {
		// 没有按名称索引, 可以认为索引已被清理, 无需继续
		return
	}

	nodeNameUidMap, ok := r.mapSvcBySTpAndSName[serviceType]
	if ok {
		nameUidMap, ok := nodeNameUidMap[serviceName]
		if ok {
			delete(nameUidMap, serviceUid)
			if len(nameUidMap) == 0 {
				delete(nodeNameUidMap, serviceName)
			}
		}
		if len(nodeNameUidMap) == 0 {
			delete(r.mapSvcBySTpAndSName, serviceType)
		}
	}
}

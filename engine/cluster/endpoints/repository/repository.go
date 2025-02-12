// Package repository
// @Title  服务存储器
// @Description  用于存放所有服务的注册信息,包括本地和远程的服务信息
// @Author  yr  2024/11/7
// @Update  yr  2024/11/7
package repository

import (
	"github.com/njtc406/emberengine/engine/inf"
	"github.com/njtc406/emberengine/engine/utils/timelib"
	"sync"
	"time"
)

type tmpInfo struct {
	sender inf.IRpcSender
	latest time.Time
}

type Repository struct {
	mapPID    *sync.Map // 服务 [serviceUid]inf.IRpcSender
	tmpMapPid *sync.Map // 临时服务 [serviceUid]tmpInfo

	ticker *time.Ticker

	// 快速查询表
	mapNodeLock          sync.RWMutex
	mapSvcBySNameAndSUid map[string]map[string]struct{}            // [serviceName]map[serviceUid]struct{}
	mapSvcBySTpAndSName  map[string]map[string]map[string]struct{} // [serviceType]map[serviceName]map[serviceUid]struct{}
}

func NewRepository() *Repository {
	return &Repository{
		mapPID:               new(sync.Map),
		tmpMapPid:            new(sync.Map),
		ticker:               time.NewTicker(time.Second * 10),
		mapSvcBySNameAndSUid: make(map[string]map[string]struct{}),
		mapSvcBySTpAndSName:  make(map[string]map[string]map[string]struct{}),
	}
}

func (r *Repository) Start() {
	r.tick()
}

func (r *Repository) Stop() {
	r.ticker.Stop()
	// 关闭所有连接
	r.mapPID.Range(func(key, value any) bool {
		if client, ok := value.(inf.IRpcSender); ok {
			client.Close()
		}
		return true
	})
}

func (r *Repository) tick() {
	go func() {
		defer func() {
			// 退出时关闭所有临时连接
			r.tmpMapPid.Range(func(key, value any) bool {
				if client, ok := value.(inf.IRpcSender); ok {
					client.Close()
				}
				return true
			})
		}()
		for {
			select {
			case _, ok := <-r.ticker.C:
				if !ok {
					return
				}
				r.tmpMapPid.Range(func(key, value any) bool {
					if tmp, ok := value.(*tmpInfo); ok {
						// 5分钟未更新则删除
						// TODO 后续根据需要调整
						if timelib.Now().Sub(tmp.latest) > time.Minute*5 {
							r.tmpMapPid.Delete(key)
						}
					}
					return true
				})
			}
		}
	}()
}

func (r *Repository) AddTmp(sender inf.IRpcSender) inf.IRpcSender {
	tmp := &tmpInfo{
		sender: sender,
		latest: timelib.Now(),
	}
	r.tmpMapPid.Store(sender.GetPid().GetServiceUid(), tmp)
	//log.SysLogger.Infof("add tmp service: %s", sender.GetPid().GetServiceUid())
	return sender
}

func (r *Repository) Add(client inf.IRpcSender) {
	oldClient, ok := r.mapPID.LoadOrStore(client.GetPid().GetServiceUid(), client)
	if ok {
		//log.SysLogger.Debugf("service already exists: %s", client.GetPid().GetServiceUid())
		oldClient.(inf.IRpcSender).Close()                      // 旧的关闭
		r.mapPID.Store(client.GetPid().GetServiceUid(), client) // 更新
		return
	}

	r.mapNodeLock.Lock()
	defer r.mapNodeLock.Unlock()

	pid := client.GetPid()
	serviceType := pid.GetServiceType()
	serviceName := pid.GetName()
	serviceUid := pid.GetServiceUid()

	nameMap, ok := r.mapSvcBySNameAndSUid[serviceName]
	if !ok {
		r.mapSvcBySNameAndSUid[serviceName] = make(map[string]struct{})
		nameMap = r.mapSvcBySNameAndSUid[serviceName]
	}

	_, ok = nameMap[serviceUid]
	if !ok {
		nameMap[serviceUid] = struct{}{}
	}

	nodeNameUidMap, ok := r.mapSvcBySTpAndSName[serviceType]
	if !ok {
		r.mapSvcBySTpAndSName[serviceType] = make(map[string]map[string]struct{})
		nodeNameUidMap = r.mapSvcBySTpAndSName[serviceType]
	}

	nameUidMap, ok := nodeNameUidMap[serviceName]
	if !ok {
		nodeNameUidMap[serviceName] = make(map[string]struct{})
		nameUidMap = nodeNameUidMap[serviceName]
	}

	_, ok = nameUidMap[serviceUid]
	if !ok {
		nameUidMap[serviceUid] = struct{}{}
	}
}

func (r *Repository) Remove(key string) {
	ret, ok := r.mapPID.LoadAndDelete(key)
	if !ok {
		return
	}
	client := ret.(inf.IRpcSender)
	pid := client.GetPid()
	client.Close()

	r.mapNodeLock.Lock()
	defer r.mapNodeLock.Unlock()
	serviceName := pid.GetName()
	serviceUid := pid.GetServiceUid()
	serviceType := pid.GetServiceType()

	nameMap, ok := r.mapSvcBySNameAndSUid[serviceName]
	if ok {
		delete(nameMap, serviceUid)
		if len(nameMap) == 0 {
			delete(r.mapSvcBySNameAndSUid, serviceName)
		}
	} else {
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

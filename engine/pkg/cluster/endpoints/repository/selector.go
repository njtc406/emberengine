// Package repository
// @Title  服务选择器
// @Description  根据条件选择服务
// @Author  yr  2024/11/7
// @Update  yr  2024/11/7
package repository

import (
	"github.com/njtc406/emberengine/engine/internal/message/msgbus"
	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/timelib"
)

func (r *Repository) SelectByServiceUid(serviceUid string) inf.IRpcDispatcher {
	v, ok := r.mapPID.Load(serviceUid)
	if ok {
		sender := v.(inf.IRpcDispatcher)
		if sender != nil && !actor.IsRetired(sender.GetPid()) {
			return sender
		}
	} else {
		tmpV, ok := r.tmpMapPid.Load(serviceUid)
		if ok {
			tmp := tmpV.(*tmpInfo)
			tmp.latest = timelib.Now()
			sender := tmp.dispatcher
			if sender != nil {
				r.tmpMapPid.Store(serviceUid, tmp)
				return sender
			}
		}
	}
	return nil
}

func (r *Repository) SelectByUid(sender *actor.PID, serviceUid string) inf.IBus {
	s := r.SelectByServiceUid(sender.GetServiceUid())
	c := r.SelectByServiceUid(serviceUid)
	if c != nil && !actor.IsRetired(c.GetPid()) {
		b := msgbus.NewMessageBus(s, c, nil)
		return b
	}
	return msgbus.NewMessageBus(s, c, def.ErrServiceNotFound)
}

func (r *Repository) Select(sender *actor.PID, options ...inf.SelectorOption) inf.IBus {
	s := r.SelectByServiceUid(sender.GetServiceUid())

	opts := &inf.SelectOption{}
	for _, opt := range options {
		opt(opts)
	}
	var returnList msgbus.MultiBus
	// 根据options筛选
	r.mapPID.Range(func(key, value any) bool {
		c := value.(inf.IRpcDispatcher)
		var chose bool
		if opts.ServiceUid != nil && key != opts.ServiceUid {
			chose = true
		}
		if chose {
			returnList = append(returnList, msgbus.NewMessageBus(s, c, nil))
		}

		return true
	})
}

func (r *Repository) SelectSlavers(sender *actor.PID, serverId int32, serviceName, serviceId string, options ...inf.SelectorOption) inf.IBus {
	return nil
}

// ===============================下面的废弃,使用上面的新接口=============================

//func (r *Repository) SelectByPid(sender, receiver *actor.PID) inf.IBus {
//	s := r.SelectByServiceUid(sender.GetServiceUid())
//	c := r.SelectByServiceUid(receiver.GetServiceUid())
//	if c != nil && !actor.IsRetired(c.GetPid()) {
//		b := msgbus.NewMessageBus(s, c, nil)
//		return b
//	}
//	return msgbus.NewMessageBus(s, c, def.ErrServiceNotFound)
//}
//
//func (r *Repository) SelectBySvcUid(sender *actor.PID, serviceUid string) inf.IBus {
//	s := r.SelectByServiceUid(sender.GetServiceUid())
//	c := r.SelectByServiceUid(serviceUid)
//
//	if c != nil && !actor.IsRetired(c.GetPid()) {
//		b := msgbus.NewMessageBus(s, c, nil)
//		return b
//	}
//	return msgbus.NewMessageBus(s, c, def.ErrServiceNotFound)
//}
//
//func (r *Repository) SelectByRule(sender *actor.PID, rule func(pid *actor.PID) bool) inf.IBus {
//	s := r.SelectByServiceUid(sender.GetServiceUid())
//	var returnList msgbus.MultiBus
//	r.mapPID.Range(func(key, value any) bool {
//		if rule(value.(inf.IRpcDispatcher).GetPid()) {
//			returnList = append(returnList, msgbus.NewMessageBus(s, value.(inf.IRpcDispatcher), nil))
//		}
//		return true
//	})
//
//	return returnList
//}
//
//func (r *Repository) Select(sender *actor.PID, serverId int32, serviceId, serviceName string) inf.IBus {
//	r.mapNodeLock.RLock(sender.GetServiceUid())
//	defer r.mapNodeLock.RUnlock(sender.GetServiceUid())
//	serviceUid := actor.CreateServiceUid(serverId, serviceName, serviceId)
//	return r.SelectBySvcUid(sender, serviceUid)
//}
//
//func (r *Repository) SelectByServiceType(sender *actor.PID, serverId int32, serviceType, serviceName string) inf.IBus {
//	if serviceType == "" && serviceName == "" {
//		return msgbus.MultiBus{}
//	}
//	r.mapNodeLock.RLock(sender.GetServiceUid())
//	defer r.mapNodeLock.RUnlock(sender.GetServiceUid())
//
//	var list msgbus.MultiBus
//	var serviceList []string
//	if serviceType == "" {
//		for _, nameUidMap := range r.mapSvcBySTpAndSName {
//			uidMap, ok := nameUidMap[serviceName] // serviceType 和 serviceName不能同时为空,所以这里必定有serviceName
//			if !ok {
//				continue
//			}
//			for serviceUid := range uidMap {
//				serviceList = append(serviceList, serviceUid)
//			}
//		}
//	} else {
//		nameUidMap, ok := r.mapSvcBySTpAndSName[serviceType]
//		if !ok {
//			return list
//		}
//
//		if serviceName == "" {
//			for _, uidMap := range nameUidMap {
//				for serviceUid := range uidMap {
//					serviceList = append(serviceList, serviceUid)
//				}
//			}
//		} else {
//			uidMap, ok := nameUidMap[serviceName]
//			if !ok {
//				return list
//			}
//			for serviceUid := range uidMap {
//				serviceList = append(serviceList, serviceUid)
//			}
//		}
//	}
//
//	s := r.SelectByServiceUid(sender.GetServiceUid())
//
//	for _, serviceUid := range serviceList {
//		c := r.SelectByServiceUid(serviceUid)
//		if c != nil && !actor.IsRetired(c.GetPid()) && (serverId == 0 || c.GetPid().GetServerId() == serverId) {
//			list = append(list, msgbus.NewMessageBus(s, c, nil))
//		}
//	}
//
//	return list
//}
//
//func (r *Repository) SelectByFilterAndChoice(sender *actor.PID, filter func(pid *actor.PID) bool, choice func(pids []*actor.PID) []*actor.PID) inf.IBus {
//	s := r.SelectByServiceUid(sender.GetServiceUid())
//	var tmpList []*actor.PID
//	r.mapPID.Range(func(key, value any) bool {
//		if filter(value.(inf.IRpcDispatcher).GetPid()) {
//			tmpList = append(tmpList, value.(inf.IRpcDispatcher).GetPid())
//		}
//		return true
//	})
//
//	list := choice(tmpList)
//	var returnList msgbus.MultiBus
//	for _, pid := range list {
//		c := r.SelectByServiceUid(pid.GetServiceUid())
//		if c != nil && !actor.IsRetired(c.GetPid()) {
//			returnList = append(returnList, msgbus.NewMessageBus(s, c, nil))
//		}
//	}
//
//	return returnList
//}

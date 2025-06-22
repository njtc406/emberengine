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

//
//func (r *Repository) FindOne(sender *actor.PID, serviceUid string) inf.IBus {
//	s := r.SelectByServiceUid(sender.GetServiceUid())
//	c := r.SelectByServiceUid(serviceUid)
//	if c != nil && !actor.IsRetired(c.GetPid()) {
//		b := msgbus.NewMessageBus(s, c, nil)
//		return b
//	}
//	return msgbus.NewMessageBus(s, c, def.ErrServiceNotFound)
//}
//
//func (r *Repository) Select(sender *actor.PID, builders ...inf.CriteriaBuilder) inf.IBus {
//	s := r.SelectByServiceUid(sender.GetServiceUid())
//
//	criteria := &inf.FilterCriteria{}
//	for _, build := range builders {
//		build(criteria)
//	}
//
//	var returnList msgbus.MultiBus
//	r.mapPID.Range(func(_, value any) bool {
//		c := value.(inf.IRpcDispatcher)
//		pid := c.GetPid()
//
//		for _, pred := range criteria.Predicates {
//			if !pred(pid) {
//				return true
//			}
//		}
//
//		returnList = append(returnList, msgbus.NewMessageBus(s, c, nil))
//		return true
//	})
//
//	return returnList
//}
//
//func (r *Repository) SelectSlavers(sender *actor.PID, serverId int32, serviceName, serviceId string, options ...inf.SelectorOption) inf.IBus {
//	return nil
//}

// ===============================下面的废弃,使用上面的新接口=============================

func (r *Repository) SelectByPid(sender, receiver *actor.PID) inf.IBus {
	s := r.SelectByServiceUid(sender.GetServiceUid())
	c := r.SelectByServiceUid(receiver.GetServiceUid())
	if c != nil && !actor.IsRetired(c.GetPid()) {
		b := msgbus.NewMessageBus(s, c, nil)
		return b
	}
	return msgbus.NewMessageBus(s, c, def.ErrServiceNotFound)
}

func (r *Repository) SelectBySvcUid(sender *actor.PID, serviceUid string) inf.IBus {
	s := r.SelectByServiceUid(sender.GetServiceUid())
	c := r.SelectByServiceUid(serviceUid)

	if c != nil && !actor.IsRetired(c.GetPid()) {
		b := msgbus.NewMessageBus(s, c, nil)
		return b
	}
	return msgbus.NewMessageBus(s, c, def.ErrServiceNotFound)
}

func (r *Repository) SelectByRule(sender *actor.PID, rule func(pid *actor.PID) bool) inf.IBus {
	s := r.SelectByServiceUid(sender.GetServiceUid())
	var returnList msgbus.MultiBus
	r.mapPID.Range(func(key, value any) bool {
		c := value.(inf.IRpcDispatcher)
		pid := c.GetPid()
		if pid.GetIsMaster() && rule(pid) {
			returnList = append(returnList, msgbus.NewMessageBus(s, value.(inf.IRpcDispatcher), nil))
		}
		return true
	})

	return returnList
}

func (r *Repository) Select(sender *actor.PID, options ...inf.SelectParamBuilder) inf.IBus {
	s := r.SelectByServiceUid(sender.GetServiceUid())
	r.mapNodeLock.RLock(sender.GetServiceUid())
	defer r.mapNodeLock.RUnlock(sender.GetServiceUid())

	param := &inf.SelectParam{}
	for _, build := range options {
		build(param)
	}

	nameUidMap, ok := r.mapSvcBySNameAndSUid[*param.ServiceName] // 不做判断,如果没给serviceName直接崩,免得忘写
	if !ok {
		return msgbus.NewMessageBus(s, nil, def.ErrServiceNotFound)
	}

	var returnList msgbus.MultiBus
	for serviceUid, _ := range nameUidMap {
		c := r.SelectByServiceUid(serviceUid)
		cPid := c.GetPid()
		// 常规选择只选择主服务
		if c != nil && !actor.IsRetired(cPid) && (param.ServerId == nil || cPid.GetServerId() == *param.ServerId) && (param.ServiceId == nil || cPid.GetServiceId() == *param.ServiceId) && cPid.GetIsMaster() {
			returnList = append(returnList, msgbus.NewMessageBus(s, c, nil))
		}
	}

	return returnList
}

func (r *Repository) SelectByServiceType(sender *actor.PID, serverId int32, serviceType, serviceName string) inf.IBus {
	if serviceType == "" && serviceName == "" {
		return msgbus.MultiBus{}
	}
	r.mapNodeLock.RLock(sender.GetServiceUid())
	defer r.mapNodeLock.RUnlock(sender.GetServiceUid())

	var list msgbus.MultiBus
	var serviceList []string
	if serviceType == "" {
		for _, nameUidMap := range r.mapSvcBySTpAndSName {
			uidMap, ok := nameUidMap[serviceName] // serviceType 和 serviceName不能同时为空,所以这里必定有serviceName
			if !ok {
				continue
			}
			for serviceUid := range uidMap {
				serviceList = append(serviceList, serviceUid)
			}
		}
	} else {
		nameUidMap, ok := r.mapSvcBySTpAndSName[serviceType]
		if !ok {
			return list
		}

		if serviceName == "" {
			for _, uidMap := range nameUidMap {
				for serviceUid := range uidMap {
					serviceList = append(serviceList, serviceUid)
				}
			}
		} else {
			uidMap, ok := nameUidMap[serviceName]
			if !ok {
				return list
			}
			for serviceUid := range uidMap {
				serviceList = append(serviceList, serviceUid)
			}
		}
	}

	s := r.SelectByServiceUid(sender.GetServiceUid())

	for _, serviceUid := range serviceList {
		c := r.SelectByServiceUid(serviceUid)
		cPid := c.GetPid()
		if c != nil && !actor.IsRetired(cPid) && (serverId == 0 || cPid.GetServerId() == serverId) && cPid.GetIsMaster() {
			list = append(list, msgbus.NewMessageBus(s, c, nil))
		}
	}

	return list
}

func (r *Repository) SelectByFilterAndChoice(sender *actor.PID, filter func(pid *actor.PID) bool, choice func(pids []*actor.PID) []*actor.PID) inf.IBus {
	s := r.SelectByServiceUid(sender.GetServiceUid())
	var tmpList []*actor.PID
	r.mapPID.Range(func(key, value any) bool {
		if filter(value.(inf.IRpcDispatcher).GetPid()) {
			tmpList = append(tmpList, value.(inf.IRpcDispatcher).GetPid())
		}
		return true
	})

	list := choice(tmpList)
	var returnList msgbus.MultiBus
	for _, pid := range list {
		c := r.SelectByServiceUid(pid.GetServiceUid())
		if c != nil && !actor.IsRetired(c.GetPid()) {
			returnList = append(returnList, msgbus.NewMessageBus(s, c, nil))
		}
	}

	return returnList
}

func (r *Repository) SelectSlavers(sender *actor.PID, options ...inf.SelectParamBuilder) inf.IBus {
	s := r.SelectByServiceUid(sender.GetServiceUid())
	r.mapNodeLock.RLock(sender.GetServiceUid())
	defer r.mapNodeLock.RUnlock(sender.GetServiceUid())

	param := &inf.SelectParam{}
	for _, build := range options {
		build(param)
	}
	var returnList msgbus.MultiBus
	if param.ServiceName == nil || param.ServerId == nil {
		return returnList
	}

	serviceName := *param.ServiceName

	nameUidMap, ok := r.mapSvcBySNameAndSUid[serviceName]
	if !ok {
		return returnList
	}

	for serviceUid, _ := range nameUidMap {
		c := r.SelectByServiceUid(serviceUid)
		cPid := c.GetPid()
		// 常规选择只选择主服务
		if c != nil && !actor.IsRetired(cPid) && (cPid.GetServerId() == *param.ServerId) && (param.ServiceId == nil || cPid.GetServiceId() == *param.ServiceId) && !cPid.GetIsMaster() {
			returnList = append(returnList, msgbus.NewMessageBus(s, c, nil))
		}
	}

	return returnList
}

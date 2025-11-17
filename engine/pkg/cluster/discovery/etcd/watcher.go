package etcd

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	disc "github.com/njtc406/emberengine/engine/pkg/cluster/discovery"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/util"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type watcher struct {
	svc inf.IService
	d   *EtcdDiscovery

	leaseRef disc.LeaseRef
	isMaster atomic.Bool
	started  atomic.Bool

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	watchMasterCtx    context.Context
	watchMasterCancel context.CancelFunc
	watchMasterWg     sync.WaitGroup
}

func newWatcher(svc inf.IService, d *EtcdDiscovery) *watcher {
	ctx, cancel := context.WithCancel(context.Background())
	return &watcher{svc: svc, d: d, ctx: ctx, cancel: cancel}
}

func (w *watcher) Start() error {
	if !w.started.CompareAndSwap(false, true) {
		return nil
	}
	if err := w.initLease(); err != nil {
		return fmt.Errorf("init lease failed: %w", err)
	}
	w.wg.Add(1)
	go w.keepaliveLoop()
	return w.electMaster()
}

func (w *watcher) Stop() {
	if w.started.CompareAndSwap(true, false) {
		w.cancel()
		w.releaseLease()
		w.stopWatchMaster()
		w.wg.Wait()
	}
}

func (w *watcher) Restart() {
	go func() {
		const maxBackoff = 30 * time.Second
		const maxRetry = 5
		for retryCount := 0; retryCount < maxRetry; retryCount++ {
			w.Stop()
			err := w.Start()
			if err == nil {
				return
			}
			log.SysLogger.Warnf("watcher start failed, retry %d/%d: %v", retryCount+1, maxRetry, err)
			backoff := time.Duration(1<<retryCount) * time.Second
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			backoff += time.Duration(util.RandN(1000)) * time.Millisecond
			time.Sleep(backoff)
		}
		log.SysLogger.Errorf("watcher start failed after %d retries", maxRetry)
		evt := event.NewEvent()
		evt.Type = event.ServiceDisconnected
		evt.SetHeader(def.DefaultPriorityKey, def.PrioritySys)
		if pushErr := w.svc.PushEvent(evt); pushErr != nil {
			log.SysLogger.Errorf("failed to notify service disconnection: %v", pushErr)
		}
	}()
}

func (w *watcher) IsMaster() bool { return w.isMaster.Load() }

func (w *watcher) initLease() error {
	if !w.d.provider.IsConnected() {
		return fmt.Errorf("etcd client is not connected")
	}
	respRef, err := w.d.leaseMgr.Grant(w.d.conf.TTL)
	if err != nil {
		return fmt.Errorf("create lease failed: %w", err)
	}
	w.releaseLease()
	w.leaseRef = respRef
	return nil
}

func (w *watcher) releaseLease() {
	if w.leaseRef != nil {
		w.d.leaseMgr.Revoke(w.leaseRef)
		w.leaseRef = nil
	}
}

func (w *watcher) keepaliveLoop() {
	defer w.wg.Done()
	var retryCount int
	const maxBackoff = 30 * time.Second
	for {
		select {
		case <-w.ctx.Done():
			log.SysLogger.Debugf("watcher[%s] exit", w.svc.GetPid().GetServiceUid())
			return
		default:
			w.keepalive()
			if !w.started.Load() {
				return
			}
			if err := w.initLease(); err != nil {
				log.SysLogger.Warnf("init pid[%s] lease error: %v", w.svc.GetPid().GetServiceUid(), err)
				retryCount++
				backoff := time.Duration(1<<retryCount) * time.Second
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				backoff += time.Duration(util.RandN(1000)) * time.Millisecond
				time.Sleep(backoff)
				continue
			}
			if err := w.electMaster(); err != nil {
				log.SysLogger.Errorf("elect master error: %v", err)
			} else {
				retryCount = 0
			}
		}
	}
}

func (w *watcher) keepalive() {
	if w.leaseRef == nil {
		return
	}
	if !w.d.provider.IsConnected() {
		return
	}
	if err := w.d.leaseMgr.KeepAliveLoop(w.ctx, w.leaseRef); err != nil {
		log.SysLogger.Errorf("etcd keepalive failed: %v", err)
		return
	}
}

func (w *watcher) electMaster() (err error) {
	if !w.svc.IsPrimarySecondaryMode() {
		w.isMaster.Store(true)
		w.svc.GetPid().SetMaster(true)
		if err = w.registerService(); err != nil {
			log.SysLogger.Errorf("register service to etcd failed: %v\n stack:%s", err, debug.Stack())
			return err
		}
		return
	}
	pid := w.svc.GetPid()
	masterKey := w.d.registry.MasterKey(pid.GetServiceGroup())
	w.stopWatchMaster()
	isMaster := w.IsMaster()
	w.isMaster.Store(false)
	pid.SetMaster(false)
	if isMaster {
		w.notifyService(event.ServiceLoseMaster, isMaster)
	}
	if !w.d.provider.IsConnected() {
		return fmt.Errorf("discovery registerService: etcd connect failed")
	}
	defer func() {
		if err = w.registerService(); err != nil {
			log.SysLogger.Errorf("register service to etcd failed: %v", err)
		}
	}()
	succeeded, respErr := w.d.election.TryAcquireMaster(w.ctx, masterKey, pid.GetServiceGroup(), w.leaseRef)
	if respErr != nil {
		log.SysLogger.Errorf("master election txn error: %v", respErr)
		goto Slave
	}
	if succeeded {
		w.isMaster.Store(true)
		pid.SetMaster(true)
		w.notifyService(event.ServiceBecomeMaster, isMaster)
		return
	}
Slave:
	w.watchMasterWg.Add(1)
	go w.startWatchMaster(masterKey)
	w.notifyService(event.ServiceBecomeSlaver, isMaster)
	return
}

func (w *watcher) notifyService(evtType int32, oldStateIsMaster bool) {
	evt := event.NewEvent()
	evt.Type = evtType
	evt.SetHeader(def.DefaultPriorityKey, def.PrioritySys)
	evt.Data = oldStateIsMaster
	if err := w.svc.PushEvent(evt); err != nil {
		log.SysLogger.Errorf("push event[%d] error: %v", evtType, err)
	}
}

func (w *watcher) registerService() error {
	if !w.d.provider.IsConnected() {
		return fmt.Errorf("etcd client not connected")
	}
	pid := w.svc.GetPid()
	return w.d.registry.RegisterService(w.ctx, pid, w.leaseRef)
}

func (w *watcher) startWatchMaster(masterKey string) {
	defer w.watchMasterWg.Done()
	ctx, cancel := context.WithCancel(w.ctx)
	w.watchMasterCtx, w.watchMasterCancel = ctx, cancel
	watchChan := w.d.provider.Watch(ctx, masterKey)
	for {
		select {
		case <-w.watchMasterCtx.Done():
			return
		case resp := <-watchChan:
			if resp.Err() != nil {
				log.SysLogger.Errorf("watch master error: %v", resp.Err())
				go w.electMaster()
				return
			}
			for _, ev := range resp.Events {
				if ev.Type == clientv3.EventTypeDelete {
					log.SysLogger.Debugf("master node lost, re-electing")
					go w.electMaster()
					return
				}
			}
		}
	}
}

func (w *watcher) stopWatchMaster() {
	if w.watchMasterCancel != nil {
		w.watchMasterCancel()
		w.watchMasterWg.Wait()
	}
}

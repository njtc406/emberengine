// Package discovery
// @Title  title
// @Description  desc
// @Author  yr  2025/6/20
// @Update  yr  2025/6/20
package discovery

import (
	"context"
	"fmt"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/util"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/protobuf/encoding/protojson"
	"path"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

type watcher struct {
	svc       inf.IService
	discovery *EtcdDiscovery

	leaseID  clientv3.LeaseID
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
	return &watcher{
		svc:       svc,
		discovery: d,
		ctx:       ctx,
		cancel:    cancel,
	}
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
		w.stopWatchMaster()
		w.releaseLease()
		w.wg.Wait()
	}
}

func (w *watcher) Restart() {
	go func() {
		const maxBackoff = 30 * time.Second
		const maxRetry = 5

		// 循环尝试重启
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

		// 超过最大重试次数，通知服务断开
		log.SysLogger.Errorf("watcher start failed after %d retries", maxRetry)
		evt := event.NewEvent()
		evt.Type = event.ServiceDisconnected
		evt.Priority = def.PrioritySys
		if pushErr := w.svc.PushEvent(evt); pushErr != nil {
			log.SysLogger.Errorf("failed to notify service disconnection: %v", pushErr)
		}
	}()
}

func (w *watcher) IsMaster() bool {
	return w.isMaster.Load()
}

func (w *watcher) initLease() error {
	if !isEtcdClientConnected(w.discovery.client) {
		return fmt.Errorf("etcd client is not connected")
	}
	resp, err := w.discovery.client.Grant(context.Background(), w.discovery.conf.DiscoveryConf.TTL)
	if err != nil {
		return fmt.Errorf("create lease failed: %w", err)
	}
	w.releaseLease()
	w.leaseID = resp.ID
	return nil
}

func (w *watcher) releaseLease() {
	if w.leaseID != 0 {
		if !isEtcdClientConnected(w.discovery.client) {
			return
		}
		_, _ = w.discovery.client.Revoke(context.Background(), w.leaseID)
		w.leaseID = 0
	}
}

func (w *watcher) keepaliveLoop() {
	defer w.wg.Done()

	var retryCount int
	const maxBackoff = 30 * time.Second

	for {
		select {
		case <-w.ctx.Done():
			log.SysLogger.Infof("watcher[%s] exit", w.svc.GetPid().GetServiceUid())
			return
		default:
			w.keepalive()

			if !w.started.Load() {
				return
			}

			if err := w.initLease(); err != nil {
				log.SysLogger.Warnf("init pid[%s] lease error: %v", w.svc.GetPid().GetServiceUid(), err)

				retryCount++
				backoff := time.Duration(1<<retryCount) * time.Second // 2^retryCount
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				backoff += time.Duration(util.RandN(1000)) * time.Millisecond // 增加随机抖动
				time.Sleep(backoff)
				continue
			}
			if err := w.electMaster(); err != nil {
				log.SysLogger.Errorf("elect master error: %v", err)
			} else {
				retryCount = 0 // 成功后重置
			}
		}
	}
}

func (w *watcher) keepalive() {
	if w.leaseID <= 0 {
		return
	}
	if !isEtcdClientConnected(w.discovery.client) {
		return
	}
	kaRespCh, err := w.discovery.client.KeepAlive(w.ctx, w.leaseID)
	if err != nil {
		log.SysLogger.Errorf("etcd keepalive failed: %v", err)
		return
	}

	for {
		select {
		case <-w.ctx.Done():
			return
		case kaResp, ok := <-kaRespCh:
			if !ok || kaResp == nil {
				log.SysLogger.Warn("keepalive channel closed")
				return
			}
		}
	}
}

func (w *watcher) electMaster() (err error) {
	if !w.svc.IsPrimarySecondaryMode() {
		// 单节点模式
		w.isMaster.Store(true)
		w.svc.GetPid().SetMaster(true)
		if err = w.registerService(); err != nil {
			log.SysLogger.Errorf("register service to etcd failed: %v\n stack:%s", err, debug.Stack())
			return err
		}

		// 通知主状态
		w.notifyService(event.ServiceBecomeMaster)
		return
	}

	pid := w.svc.GetPid()
	masterKey := path.Join(w.discovery.conf.DiscoveryConf.MasterPath, pid.GetServiceGroup()) // 使用分组key,主从是相同的
	w.stopWatchMaster()

	isMaster := w.IsMaster()

	w.isMaster.Store(false)
	pid.SetMaster(false)

	if isMaster {
		// 原来是主服务,现在降级
		w.notifyService(event.ServiceLoseMaster)
	}

	if !isEtcdClientConnected(w.discovery.client) {
		return fmt.Errorf("discovery registerService: etcd connect failed")
	}

	defer func() {
		// pid发生变化,都需要重新注册
		if err = w.registerService(); err != nil {
			log.SysLogger.Errorf("register service to etcd failed: %v", err)
		}
	}()

	txnResp, respErr := w.discovery.client.Txn(w.ctx).
		If(clientv3.Compare(clientv3.CreateRevision(masterKey), "=", 0)).
		Then(clientv3.OpPut(masterKey, pid.GetServiceGroup(), clientv3.WithLease(w.leaseID))).
		Commit()
	if respErr != nil {
		log.SysLogger.Errorf("master election txn error: %v", respErr)
		goto Slave
	}

	if txnResp.Succeeded {
		w.isMaster.Store(true)
		pid.SetMaster(true)
		// 通知主状态
		w.notifyService(event.ServiceBecomeMaster)
		return
	}

Slave:
	w.watchMasterWg.Add(1)
	go w.startWatchMaster(masterKey)
	w.notifyService(event.ServiceBecomeSlaver)
	return
}

func (w *watcher) notifyService(evtType int32) {
	evt := event.NewEvent()
	evt.Type = evtType
	evt.Priority = def.PrioritySys
	if err := w.svc.PushEvent(evt); err != nil {
		log.SysLogger.Errorf("push event[%d] error: %v", evtType, err)
	}
}

func (w *watcher) registerService() error {
	if !isEtcdClientConnected(w.discovery.client) {
		return fmt.Errorf("etcd client not connected")
	}
	pid := w.svc.GetPid()
	servicePath := path.Join(w.discovery.conf.DiscoveryConf.Path, pid.GetServiceUid())
	pidData, err := protojson.Marshal(pid)
	if err != nil {
		return fmt.Errorf("marshal pid failed: %w", err)
	}
	_, err = w.discovery.client.Put(w.ctx, servicePath, string(pidData), clientv3.WithLease(w.leaseID))
	return err
}

func (w *watcher) startWatchMaster(masterKey string) {
	defer w.watchMasterWg.Done()
	ctx, cancel := context.WithCancel(w.ctx)
	w.watchMasterCtx, w.watchMasterCancel = ctx, cancel
	watchChan := w.discovery.watchKey(ctx, masterKey)

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
					log.SysLogger.Infof("master node lost, re-electing")
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

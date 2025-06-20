// Package discovery
// @Title  title
// @Description  desc
// @Author  yr  2025/6/20
// @Update  yr  2025/6/20
package discovery

import (
	"context"
	"fmt"
	"path"
	"sync"
	"sync/atomic"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/util"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/protobuf/encoding/protojson"
)

type serviceWatcher struct {
	pid       *actor.PID
	discovery *EtcdDiscovery

	leaseID  clientv3.LeaseID
	isMaster atomic.Bool
	closed   atomic.Bool

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	watchCtx    context.Context
	watchCancel context.CancelFunc
	watchWg     sync.WaitGroup
}

func newServiceWatcher(pid *actor.PID, d *EtcdDiscovery) *serviceWatcher {
	ctx, cancel := context.WithCancel(context.Background())
	return &serviceWatcher{
		pid:       pid,
		discovery: d,
		ctx:       ctx,
		cancel:    cancel,
	}
}

func (w *serviceWatcher) Start() error {
	if w.closed.Load() {
		return nil
	}
	if err := w.initLease(); err != nil {
		return fmt.Errorf("init lease failed: %w", err)
	}
	w.wg.Add(1)
	go w.keepaliveLoop()
	return nil
}

func (w *serviceWatcher) Stop() {
	if w.closed.CompareAndSwap(false, true) {
		w.stopWatchMaster()
		w.releaseLease()
		w.cancel()
		w.wg.Wait()
	}
}

func (w *serviceWatcher) IsMaster() bool {
	return w.isMaster.Load()
}

func (w *serviceWatcher) initLease() error {
	resp, err := w.discovery.client.Grant(context.Background(), w.discovery.conf.DiscoveryConf.TTL)
	if err != nil {
		return fmt.Errorf("create lease failed: %w", err)
	}
	w.releaseLease()
	w.leaseID = resp.ID
	return nil
}

func (w *serviceWatcher) releaseLease() {
	if w.leaseID != 0 {
		_, _ = w.discovery.client.Revoke(context.Background(), w.leaseID)
		w.leaseID = 0
	}
}

func (w *serviceWatcher) keepaliveLoop() {
	defer func() {
		//log.TryLogPanic("etcd keepalive")
		w.wg.Done()
	}()

	for {
		select {
		case <-w.ctx.Done():
			return
		default:
			w.keepalive()
			if err := w.initLease(); err != nil {
				log.SysLogger.Errorf("init lease error: %v", err)
				continue
			}
			time.Sleep(time.Duration(util.RandN(5)) * time.Second)
		}
	}
}

func (w *serviceWatcher) keepalive() {
	if w.leaseID <= 0 {
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

func (w *serviceWatcher) electMaster() {
	masterKey := path.Join(w.discovery.conf.DiscoveryConf.MasterPath, w.pid.GetServiceUid())
	w.stopWatchMaster()

	w.isMaster.Store(false)
	w.pid.SetMaster(false)

	txnResp, err := w.discovery.client.Txn(context.Background()).
		If(clientv3.Compare(clientv3.CreateRevision(masterKey), "=", 0)).
		Then(clientv3.OpPut(masterKey, w.pid.GetServiceUid(), clientv3.WithLease(w.leaseID))).
		Commit()
	if err != nil {
		log.SysLogger.Errorf("master election txn error: %v", err)
		goto Slave
	}

	if txnResp.Succeeded {
		w.isMaster.Store(true)
		w.pid.SetMaster(true)
		_ = w.registerService()
		return
	}

Slave:
	w.watchWg.Add(1)
	go w.startWatchMaster(masterKey)
}

func (w *serviceWatcher) registerService() error {
	servicePath := path.Join(w.discovery.conf.DiscoveryConf.Path, w.pid.GetServiceUid())
	pidData, err := protojson.Marshal(w.pid)
	if err != nil {
		return fmt.Errorf("marshal pid failed: %w", err)
	}
	_, err = w.discovery.client.Put(context.Background(), servicePath, string(pidData), clientv3.WithLease(w.leaseID))
	return err
}

func (w *serviceWatcher) startWatchMaster(masterKey string) {
	defer w.watchWg.Done()
	ctx, cancel := context.WithCancel(context.Background())
	w.watchCtx, w.watchCancel = ctx, cancel
	watchChan := w.discovery.watchKey(ctx, masterKey)

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-w.watchCtx.Done():
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

func (w *serviceWatcher) stopWatchMaster() {
	if w.watchCancel != nil {
		w.watchCancel()
		w.watchWg.Wait()
	}
}

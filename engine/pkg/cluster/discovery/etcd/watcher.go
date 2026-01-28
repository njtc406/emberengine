package etcd

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/actor/mailbox/job"
	disc "github.com/njtc406/emberengine/engine/pkg/cluster/discovery"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/codec"
	"github.com/njtc406/emberengine/engine/pkg/utils/idle"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// 默认恢复配置常量
const (
	defaultBackoffBaseDelay = 1 * time.Second  // 默认退避基础延迟
	defaultBackoffMaxDelay  = 30 * time.Second // 默认退避最大延迟
	defaultVerboseLogCount  = 5                // 默认前N次详细日志
	defaultLogInterval      = 10               // 默认日志间隔
)

type watcher struct {
	svc inf.IService
	d   *EtcdDiscovery

	leaseRef    disc.LeaseRef
	isMaster    atomic.Bool
	masterEpoch atomic.Int64
	started     atomic.Bool

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

// getRecoveryConf 获取故障恢复配置，如果未配置则返回默认值
func (w *watcher) getRecoveryConf() *config.DiscoveryRecoveryConf {
	if w.d.conf.RecoveryConf != nil {
		return w.d.conf.RecoveryConf
	}
	// 返回默认配置
	return &config.DiscoveryRecoveryConf{
		BackoffBaseDelay: defaultBackoffBaseDelay,
		BackoffMaxDelay:  defaultBackoffMaxDelay,
		VerboseLogCount:  defaultVerboseLogCount,
		LogInterval:      defaultLogInterval,
	}
}

// newBackoff 根据配置创建退避策略
func (w *watcher) newBackoff() *idle.ExponentialBackoff {
	conf := w.getRecoveryConf()
	baseDelay := conf.BackoffBaseDelay
	maxDelay := conf.BackoffMaxDelay

	// 确保有效值
	if baseDelay <= 0 {
		baseDelay = defaultBackoffBaseDelay
	}
	if maxDelay <= 0 {
		maxDelay = defaultBackoffMaxDelay
	}

	// maxRetries=0 表示无限重试
	return idle.NewExponentialBackoff(baseDelay, maxDelay, 0)
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
		// 从配置获取退避参数
		recoveryConf := w.getRecoveryConf()
		backoff := w.newBackoff()

		// 日志控制参数
		verboseLogCount := recoveryConf.VerboseLogCount
		logInterval := recoveryConf.LogInterval
		if verboseLogCount <= 0 {
			verboseLogCount = defaultVerboseLogCount
		}
		if logInterval <= 0 {
			logInterval = defaultLogInterval
		}

		var retryCount int

		for {
			// 检查是否应该退出
			select {
			case <-w.ctx.Done():
				log.SysLogger.Infof("watcher[%s] restart cancelled", w.svc.GetPid().GetServiceUid())
				return
			default:
			}

			w.Stop()
			err := w.Start()
			if err == nil {
				if retryCount > 0 {
					log.SysLogger.Infof("watcher[%s] reconnected successfully after %d retries", w.svc.GetPid().GetServiceUid(), retryCount)
				}
				return
			}

			retryCount++
			delay := backoff.NextDelay()

			// 控制日志频率：前 verboseLogCount 次每次都打印，之后每 logInterval 次打印一次
			if retryCount <= verboseLogCount || retryCount%logInterval == 0 {
				log.SysLogger.Warnf("watcher[%s] start failed (retry #%d), next attempt in %v: %v",
					w.svc.GetPid().GetServiceUid(), retryCount, delay, err)
			}

			// 带超时的等待，支持提前退出
			select {
			case <-w.ctx.Done():
				log.SysLogger.Infof("watcher[%s] restart cancelled during backoff", w.svc.GetPid().GetServiceUid())
				return
			case <-time.After(delay):
				// 继续重试
			}
		}
	}()
}

func (w *watcher) IsMaster() bool { return w.isMaster.Load() }

func (w *watcher) MasterEpoch() int64 { return w.masterEpoch.Load() }

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

	// 使用指数退避策略处理 lease 初始化失败的情况
	backoff := w.newBackoff()

	for {
		select {
		case <-w.ctx.Done():
			log.SysLogger.Debugf("watcher[%s] exit", w.svc.GetPid().GetServiceUid())
			return
		default:
			// 执行 keepalive，内部会阻塞直到 lease 过期或出错
			w.keepalive()

			// 检查是否已停止
			if !w.started.Load() {
				return
			}

			// 尝试重新初始化 lease
			if err := w.initLease(); err != nil {
				log.SysLogger.Warnf("init pid[%s] lease error: %v", w.svc.GetPid().GetServiceUid(), err)
				delay := backoff.NextDelay()

				// 带超时的等待，支持提前退出
				select {
				case <-w.ctx.Done():
					return
				case <-time.After(delay):
					continue
				}
			}

			// lease 初始化成功，重置退避计数器并重新选举
			backoff.Reset()
			if err := w.electMaster(); err != nil {
				log.SysLogger.Errorf("elect master error: %v", err)
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
		// If we were master, step down immediately to minimize overlapping work windows.
		if w.IsMaster() {
			pid := w.svc.GetPid()
			prevEpoch := w.MasterEpoch()
			w.isMaster.Store(false)
			w.masterEpoch.Store(0)
			pid.SetMaster(false)
			w.notifyService(event.ServiceLoseMaster, true, prevEpoch, 0)
		}
		return
	}
}

func (w *watcher) electMaster() (err error) {
	if !w.svc.IsPrimarySecondaryMode() {
		w.isMaster.Store(true)
		w.masterEpoch.Store(0)
		w.svc.GetPid().SetMaster(true)
		if err = w.registerService(); err != nil {
			log.SysLogger.Errorf("register service to etcd failed: %v\n stack:%s", err, debug.Stack())
			return err
		}
		return
	}
	pid := w.svc.GetPid()
	masterKey := w.d.registry.MasterKey(pid.GetPrimarySecondaryKey())
	w.stopWatchMaster()
	wasMaster := w.IsMaster()
	prevEpoch := w.MasterEpoch()
	w.isMaster.Store(false)
	w.masterEpoch.Store(0)
	pid.SetMaster(false)
	if wasMaster {
		w.notifyService(event.ServiceLoseMaster, true, prevEpoch, 0)
	}
	if !w.d.provider.IsConnected() {
		return fmt.Errorf("discovery registerService: etcd connect failed")
	}
	defer func() {
		if err = w.registerService(); err != nil {
			log.SysLogger.Errorf("register service to etcd failed: %v", err)
		}
	}()
	succeeded, epoch, respErr := w.d.election.TryAcquireMaster(w.ctx, masterKey, pid.GetPrimarySecondaryKey(), w.leaseRef)
	if respErr != nil {
		log.SysLogger.Errorf("master election txn error: %v", respErr)
		goto Slave
	}
	if succeeded {
		w.isMaster.Store(true)
		w.masterEpoch.Store(epoch)
		pid.SetMaster(true)
		w.notifyService(event.ServiceBecomeMaster, wasMaster, prevEpoch, epoch)
		return
	}
Slave:
	w.watchMasterWg.Add(1)
	go w.startWatchMaster(masterKey)
	w.notifyService(event.ServiceBecomeSlaver, wasMaster, prevEpoch, 0)
	return
}

func (w *watcher) notifyService(evtType def.EventType, oldStateIsMaster bool, prevEpoch, newEpoch int64) {
	//evt := event.NewEvent()
	//evt.Type = evtType
	//evt.Priority = def.PrioritySys
	//evt.Data = &event.MasterStateData{
	//	OldStateIsMaster: oldStateIsMaster,
	//	PrevEpoch:        prevEpoch,
	//	NewEpoch:         newEpoch,
	//}

	evt := &actor.Event{
		Type: int32(evtType),
	}

	data := &actor.MasterStateData{
		OldStateIsMaster: oldStateIsMaster,
		PrevEpoch:        prevEpoch,
		NewEpoch:         newEpoch,
	}

	dataAny, err := codec.EncodeToAny(data)
	if err != nil {
		log.SysLogger.Errorf("encode data to any error: %v", err)
		return
	}
	evt.Payload = dataAny

	j := job.NewEventBusJob()
	j.SetPriority(def.PrioritySys)
	j.SetPayload(evt)

	if err = w.svc.PostJob(j); err != nil {
		log.SysLogger.Errorf("post job error: %v", err)
		j.Release()
		return
	}
}

func (w *watcher) registerService() error {
	if !w.d.provider.IsConnected() {
		return fmt.Errorf("etcd client not connected")
	}
	pid := w.svc.GetPid()
	if pid == nil {
		return fmt.Errorf("service PID is nil")
	}
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
				go func() {
					if err := w.electMaster(); err != nil {
						log.SysLogger.Errorf("elect master error: %v", err)
					}
				}()
				return
			}
			for _, ev := range resp.Events {
				if ev.Type == clientv3.EventTypeDelete {
					log.SysLogger.Debugf("master node lost, re-electing")
					go func() {
						if err := w.electMaster(); err != nil {
							log.SysLogger.Errorf("elect master error: %v", err)
						}
					}()
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

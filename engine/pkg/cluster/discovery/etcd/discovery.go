package etcd

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	disc "github.com/njtc406/emberengine/engine/pkg/cluster/discovery"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
	"google.golang.org/grpc/connectivity"
)

const (
	defaultTTL       = 3
	defaultPath      = "/ember/service"
	defaultMasterKey = "/ember/master"
)

// EtcdDiscovery 具体后端实现（保持统一接口编排）
type EtcdDiscovery struct {
	conf        *config.DiscoveryConf
	etcdConf    *config.ETCDConf
	client      *clientv3.Client
	ctx         context.Context
	cancel      context.CancelFunc
	initialized atomic.Bool
	started     atomic.Bool
	watchers    sync.Map // map[string]*watcher

	// 组件接口
	watcher  disc.IDiscoveryServiceWatcher
	health   disc.IDiscoveryHealthMonitor
	leaseMgr disc.ILeaseManager
	registry disc.IServiceRegistry
	election disc.IMasterElection
	provider disc.IClientProvider
	closed   atomic.Bool

	proc    inf.IEventProcessor
	handler inf.IEventHandler
}

func NewEtcdDiscovery() *EtcdDiscovery { return &EtcdDiscovery{} }

func init() { disc.Register("etcd", NewEtcdDiscovery()) }

func (e *EtcdDiscovery) Init(proc inf.IEventProcessor, conf *config.ClusterConf) error {
	if len(conf.ETCDConf.Endpoints) == 0 {
		log.SysLogger.Debugf("etcd end points is empty")
		return nil
	}
	e.etcdConf = conf.ETCDConf
	log.SysLogger.Debugf("etcd discovery conf: %+v", e.etcdConf)
	e.conf = normalizeConf(conf.DiscoveryConf)
	e.proc = proc
	e.handler = event.NewHandler()
	e.handler.Init(proc)

	ctx, cancel := context.WithCancel(context.Background())
	e.ctx = ctx
	e.cancel = cancel

	if err := e.connect(); err != nil {
		log.SysLogger.Errorf("etcd discovery init failed: %v, endpoints: %v", err, conf.ETCDConf.Endpoints)
		return err
	}
	e.initialized.Store(true)

	// 组装组件
	e.provider = &etcdClientProvider{d: e}
	e.watcher = &EtcdServiceWatcher{d: e}
	e.health = &EtcdHealthMonitor{d: e}
	e.leaseMgr = &etcdLeaseManager{d: e}
	e.registry = &etcdServiceRegistry{d: e}
	e.election = &etcdMasterElection{d: e}

	proc.RegEventReceiverFunc(event.SysEventServiceReg, e.handler, e.onRegister)
	proc.RegEventReceiverFunc(event.SysEventServiceDis, e.handler, e.onUnregister)
	return nil
}

func (e *EtcdDiscovery) Start() {
	if !e.started.CompareAndSwap(false, true) {
		return
	}
	if !e.isConnect() {
		return
	}
	if e.watcher != nil {
		e.watcher.Start()
	}
	if e.health != nil {
		e.health.Start()
	}
}

func (e *EtcdDiscovery) Close() {
	if !e.started.CompareAndSwap(true, false) {
		return
	}
	e.closed.Store(true)
	if e.health != nil {
		e.health.Stop()
	}
	e.cancel()
	e.watchers.Range(func(k, v any) bool {
		v.(*watcher).Stop()
		e.watchers.Delete(k)
		return true
	})
	if e.client != nil {
		_ = e.client.Close()
		e.client = nil
	}
	e.initialized.Store(false)
}

func (e *EtcdDiscovery) isConnect() bool {
	if !e.initialized.Load() || e.closed.Load() {
		log.SysLogger.Error("etcd discovery not initialized or closed")
		return false
	}
	if e.client == nil {
		log.SysLogger.Error("etcd client is nil")
		return false
	}
	return true
}

func (e *EtcdDiscovery) syncInitialState() {
	resp, err := e.provider.GetPrefix(e.ctx, e.conf.Path)
	if err != nil {
		log.SysLogger.Errorf("sync services failed: %v", err)
		return
	}
	for _, kv := range resp.Kvs {
		data := *kv
		ent := event.NewEvent()
		ent.Type = event.SysEventETCDPut
		ent.Data = &data
		if err = e.proc.PushEvent(ent); err != nil {
			log.SysLogger.Errorf("sync service error: %v", err)
		}
	}
}

func (e *EtcdDiscovery) onRegister(ev inf.IEvent) {
	if !e.started.Load() {
		return
	}
	ent := ev.(*event.Event)
	svc, ok := ent.Data.(inf.IService)
	if !ok {
		log.SysLogger.Panic("invalid service registration data")
	}
	pid := svc.GetPid()
	if _, ok = e.watchers.Load(pid.GetServiceUid()); ok {
		return
	}
	w := newWatcher(svc, e)
	e.watchers.Store(pid.GetServiceUid(), w)
	if err := w.Start(); err != nil {
		e.watchers.Delete(pid.GetServiceUid())
		log.SysLogger.Errorf("start service[%s] watcher failed: %v", svc.GetName(), err)
	}
}

func (e *EtcdDiscovery) onUnregister(ev inf.IEvent) {
	if !e.started.Load() {
		return
	}
	ent := ev.(*event.Event)
	pid := ent.Data.(*actor.PID)
	if v, ok := e.watchers.LoadAndDelete(pid.GetServiceUid()); ok {
		v.(*watcher).Stop()
	}
}

func (e *EtcdDiscovery) watchLoop() {
	watchChan := e.provider.WatchPrefix(e.ctx, e.conf.Path)
	for {
		select {
		case <-e.ctx.Done():
			return
		case resp := <-watchChan:
			if err := resp.Err(); err != nil {
				log.SysLogger.Errorf("watch error: %v", err)
				watchChan = e.provider.WatchPrefix(e.ctx, e.conf.Path)
				continue
			}
			for _, ev := range resp.Events {
				var evType int
				switch ev.Type {
				case clientv3.EventTypePut:
					evType = event.SysEventETCDPut
				case clientv3.EventTypeDelete:
					evType = event.SysEventETCDDel
				default:
					continue
				}
				data := *ev.Kv
				ent := event.NewEvent()
				ent.Type = int32(evType)
				ent.Data = &data
				if err := e.proc.PushEvent(ent); err != nil {
					log.SysLogger.Errorf("etcd event error: %v", err)
				}
			}
		}
	}
}

func (e *EtcdDiscovery) healthCheck() {
	ticker := time.NewTicker(time.Second * 10)
	defer ticker.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			if !e.provider.IsConnected() {
				e.reconnectAndRecover()
			}
		}
	}
}

func (e *EtcdDiscovery) connect() error {
	client, err := createEtcdClient(e.etcdConf)
	if err != nil {
		return err
	}
	e.client = client
	// 验证连接是否真正建立
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = client.Get(ctx, "__health_check__")
	if err != nil && err != context.DeadlineExceeded {
		log.SysLogger.Warnf("etcd connection validation warning: %v", err)
	}
	// 检查连接状态
	if !isEtcdClientConnected(client) {
		return fmt.Errorf("etcd client not connected, endpoints: %v", e.etcdConf.Endpoints)
	}
	return nil
}

func (d *EtcdDiscovery) watchKey(ctx context.Context, key string, options ...clientv3.OpOption) <-chan clientv3.WatchResponse {
	return d.client.Watch(ctx, key, options...)
}

func (e *EtcdDiscovery) reconnectAndRecover() {
	oldClient := e.client
	if err := e.connect(); err != nil {
		log.SysLogger.Errorf("etcd reconnect failed: %v", err)
		return
	}
	if oldClient != nil {
		_ = oldClient.Close()
	}
	e.watchers.Range(func(key, value any) bool { value.(*watcher).Restart(); return true })
}

// 工具
func normalizeConf(conf *config.DiscoveryConf) *config.DiscoveryConf {
	if conf == nil {
		conf = &config.DiscoveryConf{Path: defaultPath, TTL: defaultTTL, MasterPath: defaultMasterKey}
	}
	if conf.TTL == 0 {
		conf.TTL = defaultTTL
	}
	return conf
}

func createEtcdClient(conf *config.ETCDConf) (*clientv3.Client, error) {
	// 确保 DialTimeout 有合理的默认值
	dialTimeout := conf.DialTimeout
	if dialTimeout == 0 {
		dialTimeout = 3 * time.Second
	}
	cfg := clientv3.Config{
		Endpoints:   conf.Endpoints,
		DialTimeout: dialTimeout,
		Username:    conf.UserName,
		Password:    conf.Password,
	}
	var loggerCfg zap.Config
	if config.IsDebug() {
		loggerCfg = zap.NewDevelopmentConfig()
	} else {
		loggerCfg = zap.NewProductionConfig()
	}
	if conf.NoLogger {
		cfg.Logger = zap.NewNop()
	} else {
		logger, err := loggerCfg.Build()
		if err != nil {
			log.SysLogger.Errorf("failed to create etcd logger, err:%v", err)
			return nil, err
		}
		cfg.Logger = logger
	}
	return clientv3.New(cfg)
}

func isEtcdClientConnected(client *clientv3.Client) bool {
	if client == nil {
		return false
	}
	conn := client.ActiveConnection()
	if conn == nil {
		return false
	}
	state := conn.GetState()
	return state == connectivity.Ready || state == connectivity.Idle
}

// 监听组件
type EtcdServiceWatcher struct{ d *EtcdDiscovery }

func (w *EtcdServiceWatcher) Start() { go w.d.watchLoop(); w.d.syncInitialState() }

// 健康组件
type EtcdHealthMonitor struct{ d *EtcdDiscovery }

func (h *EtcdHealthMonitor) Start() { go h.d.healthCheck() }
func (h *EtcdHealthMonitor) Stop()  { /* ctx 取消即可 */ }

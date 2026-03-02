package etcd

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	disc "github.com/njtc406/emberengine/engine/pkg/cluster/discovery"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/syncx"
	"github.com/njtc406/emberengine/engine/pkg/utils/xcontext"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
	"google.golang.org/grpc/connectivity"
)

const (
	defaultTTL       = 3
	defaultPath      = "/ember/service"
	defaultMasterKey = "/ember/master"
)

var runtimeDebug bool

func SetDebug(enabled bool) {
	runtimeDebug = enabled
}

// EtcdDiscovery 具体后端实现（保持统一接口编排）
type EtcdDiscovery struct {
	*log.Logger

	conf        *config.DiscoveryConf
	etcdConf    *config.ETCDConf
	client      *clientv3.Client
	ctx         context.Context
	cancel      context.CancelFunc
	initialized atomic.Bool
	started     atomic.Bool
	watchers    *syncx.Map[string, *watcher] // map[string]*watcher

	// 组件接口
	watcher  disc.IDiscoveryServiceWatcher
	health   disc.IDiscoveryHealthMonitor
	leaseMgr disc.ILeaseManager
	registry disc.IServiceRegistry
	election disc.IMasterElection
	provider disc.IClientProvider
	closed   atomic.Bool

	handler *event.Handler
	evtCh   inf.IEventChannel
}

func NewEtcdDiscovery() *EtcdDiscovery { return &EtcdDiscovery{} }

func (e *EtcdDiscovery) SetLogger(logger *log.Logger) {
	e.Logger = logger
}

func init() {
	disc.Register("etcd", func() inf.IDiscovery {
		return NewEtcdDiscovery()
	})
}

func (e *EtcdDiscovery) Init(conf *config.ClusterConf, eventProcessor inf.IEventProcessor, evtCh inf.IEventChannel) error {
	if e.Logger == nil {
		return fmt.Errorf("etcd discovery logger is nil")
	}
	if len(conf.ETCDConf.Endpoints) == 0 {
		e.Debugf("etcd end points is empty")
		return nil
	}
	e.etcdConf = conf.ETCDConf
	e.Debugf("etcd discovery conf: %+v", e.etcdConf)
	e.conf = normalizeConf(conf.DiscoveryConf)
	e.handler = event.NewTriggerHandler()
	e.handler.Init(eventProcessor)
	e.evtCh = evtCh
	e.watchers = syncx.NewMap[string, *watcher]()

	ctx, cancel := context.WithCancel(context.Background())
	e.ctx = ctx
	e.cancel = cancel

	if err := e.connect(); err != nil {
		e.Errorf("etcd discovery init failed: %v, endpoints: %v", err, conf.ETCDConf.Endpoints)
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

	if err := event.RegisterHandler(e.handler, event.SysEventServiceReg, "service_register", e.onRegister); err != nil {
		return fmt.Errorf("register service_register error: %w", err)
	}
	if err := event.RegisterHandler(e.handler, event.SysEventServiceDis, "service_unregister", e.onUnregister); err != nil {
		return fmt.Errorf("register service_unregister error: %w", err)
	}
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
	e.watchers.Range(func(k string, v *watcher) bool {
		v.Stop()
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
		e.Error("etcd discovery not initialized or closed")
		return false
	}
	if e.client == nil {
		e.Error("etcd client is nil")
		return false
	}
	return true
}

func (e *EtcdDiscovery) syncInitialState() {
	e.Infof("syncing initial state from path: %s", e.conf.Path)
	resp, err := e.provider.GetPrefix(e.ctx, e.conf.Path)
	if err != nil {
		e.Errorf("sync services failed: %v", err)
		return
	}
	e.Infof("found %d existing services in etcd", len(resp.Kvs))
	for _, kv := range resp.Kvs {
		e.Debugf("syncing service: key=%s", string(kv.Key))
		data := *kv
		evt := event.NewDiscoveryEvent()
		evt.Context = xcontext.New(nil)
		evt.EventType = event.SysEventETCDPut
		evt.Data = &data

		if err := e.evtCh.PushEvent(evt); err != nil {
			e.WithContext(evt.Context).Errorf("push discovery event failed: %v", err)
		}
	}
}

func (e *EtcdDiscovery) onRegister(ctx context.Context, svc inf.IService) error {
	if !e.started.Load() {
		return fmt.Errorf("etcd discovery not started")
	}
	pid := svc.GetPid()
	if _, ok := e.watchers.Load(pid.GetServiceUid()); ok {
		return fmt.Errorf("service[%s] watcher already exists", svc.GetName())
	}
	w := newWatcher(svc, e)
	e.watchers.Store(pid.GetServiceUid(), w)
	if err := w.Start(); err != nil {
		e.watchers.Delete(pid.GetServiceUid())
		e.WithContext(ctx).Errorf("start service[%s] watcher failed: %v", svc.GetName(), err)
		return err
	}
	return nil
}

func (e *EtcdDiscovery) onUnregister(ctx context.Context, pid *actor.PID) error {
	if !e.started.Load() {
		e.WithContext(ctx).Errorf("etcd discovery not started")
		return fmt.Errorf("etcd discovery not started")
	}
	if v, ok := e.watchers.LoadAndDelete(pid.GetServiceUid()); ok {
		v.Stop()
	}
	return nil
}

func (e *EtcdDiscovery) watchLoop() {
	e.Infof("etcd watchLoop started, watching path: %s", e.conf.Path)
	watchChan := e.provider.WatchPrefix(e.ctx, e.conf.Path)
	for {
		select {
		case <-e.ctx.Done():
			e.Info("etcd watchLoop stopped")
			return
		case resp := <-watchChan:
			if err := resp.Err(); err != nil {
				e.Errorf("watch error: %v", err)
				watchChan = e.provider.WatchPrefix(e.ctx, e.conf.Path)
				continue
			}
			e.Debugf("etcd watch received %d events", len(resp.Events))
			for _, ev := range resp.Events {
				var evType def.EventType
				switch ev.Type {
				case clientv3.EventTypePut:
					evType = event.SysEventETCDPut
				case clientv3.EventTypeDelete:
					evType = event.SysEventETCDDel
				default:
					continue
				}
				e.Debugf("etcd event: type=%v, key=%s", ev.Type, string(ev.Kv.Key))
				data := *ev.Kv
				evt := event.NewDiscoveryEvent()
				evt.Context = xcontext.New(nil)
				evt.EventType = evType
				evt.Data = &data

				if err := e.evtCh.PushEvent(evt); err != nil {
					e.WithContext(evt.Context).Errorf("push discovery event failed: %v", err)
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
	client, err := createEtcdClient(e.etcdConf, e.Logger)
	if err != nil {
		return err
	}
	e.client = client
	// 验证连接是否真正建立
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = client.Get(ctx, "__health_check__")
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		e.Warnf("etcd connection validation warning: %v", err)
	}
	// 检查连接状态
	if !isEtcdClientConnected(client) {
		return fmt.Errorf("etcd client not connected, endpoints: %v", e.etcdConf.Endpoints)
	}
	return nil
}

func (e *EtcdDiscovery) watchKey(ctx context.Context, key string, options ...clientv3.OpOption) <-chan clientv3.WatchResponse {
	return e.client.Watch(ctx, key, options...)
}

func (e *EtcdDiscovery) reconnectAndRecover() {
	oldClient := e.client
	if err := e.connect(); err != nil {
		e.Errorf("etcd reconnect failed: %v", err)
		return
	}
	if oldClient != nil {
		_ = oldClient.Close()
	}
	e.watchers.Range(func(key string, value *watcher) bool { value.Restart(); return true })
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

func createEtcdClient(conf *config.ETCDConf, engLogger *log.Logger) (*clientv3.Client, error) {
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
	if runtimeDebug {
		loggerCfg = zap.NewDevelopmentConfig()
	} else {
		loggerCfg = zap.NewProductionConfig()
	}
	if conf.NoLogger {
		cfg.Logger = zap.NewNop()
	} else {
		zlogger, err := loggerCfg.Build()
		if err != nil {
			if engLogger != nil {
				engLogger.Errorf("failed to create etcd logger, err:%v", err)
			}
			return nil, err
		}
		cfg.Logger = zlogger
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

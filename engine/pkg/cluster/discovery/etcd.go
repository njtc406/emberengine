package discovery

import (
	"context"
	"go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
	"google.golang.org/grpc/connectivity"
	"sync"
	"sync/atomic"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
)

const (
	defaultTTL       = 3
	defaultPath      = "/ember/service"
	defaultMasterKey = "/ember/master"
)

type EtcdDiscovery struct {
	conf        *config.ClusterConf
	client      *clientv3.Client
	ctx         context.Context
	cancel      context.CancelFunc
	initialized atomic.Bool
	started     atomic.Bool
	watchers    sync.Map // map[string]*watcher

	inf.IEventProcessor
	inf.IEventHandler
}

func NewEtcdDiscovery() *EtcdDiscovery {
	return &EtcdDiscovery{}
}

func init() {
	Register("etcd", NewEtcdDiscovery())
}

func (e *EtcdDiscovery) Init(proc inf.IEventProcessor, conf *config.ClusterConf) error {
	if len(conf.ETCDConf.Endpoints) == 0 {
		// 允许不使用服务发现,所有调用服务都是本地服务
		log.SysLogger.Info("etcd end points is empty")
		return nil
	}

	e.conf = normalizeConf(conf)
	e.IEventProcessor = proc
	e.IEventHandler = event.NewHandler()
	e.IEventHandler.Init(proc)

	ctx, cancel := context.WithCancel(context.Background())
	e.ctx = ctx
	e.cancel = cancel

	if err := e.connect(); err != nil {
		return err
	}

	e.initialized.Store(true)

	proc.RegEventReceiverFunc(event.SysEventServiceReg, e.IEventHandler, e.onRegister)
	proc.RegEventReceiverFunc(event.SysEventServiceDis, e.IEventHandler, e.onUnregister)

	return nil
}

func (e *EtcdDiscovery) Start() {
	if !e.started.CompareAndSwap(false, true) {
		return
	}
	if !e.isConnect() {
		return
	}
	// 监听集群变化
	go e.watchLoop()
	// 健康检查
	go e.healthCheck()
	// 获取初始化集群数据
	e.syncInitialState()
}

func (e *EtcdDiscovery) Close() {
	if !e.started.CompareAndSwap(true, false) {
		return
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
	if !e.initialized.Load() {
		log.SysLogger.Error("etcd client is nil")
		return false
	}
	return true
}

func (e *EtcdDiscovery) syncInitialState() {
	resp, err := e.client.Get(e.ctx, e.conf.DiscoveryConf.Path, clientv3.WithPrefix())
	if err != nil {
		log.SysLogger.Errorf("sync services failed: %v", err)
		return
	}
	for _, kv := range resp.Kvs {
		data := *kv
		ent := event.NewEvent()
		ent.Type = event.SysEventETCDPut
		ent.Data = &data
		if err = e.GetEventProcessor().PushEvent(ent); err != nil {
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
		return // already registered
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
	log.SysLogger.Infof("*****************************************service[%s] unregistered", pid.GetServiceUid())
	if v, ok := e.watchers.LoadAndDelete(pid.GetServiceUid()); ok {
		v.(*watcher).Stop()
	}
}

func (e *EtcdDiscovery) watchLoop() {
	watchChan := e.watchKey(e.ctx, e.conf.DiscoveryConf.Path, clientv3.WithPrefix())
	for {
		select {
		case <-e.ctx.Done():
			return
		case resp := <-watchChan:
			if err := resp.Err(); err != nil {
				log.SysLogger.Errorf("watch error: %v", err)
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

				data := *ev.Kv // 拷贝一下数据
				ent := event.NewEvent()
				ent.Type = int32(evType)
				ent.Data = &data
				if err := e.GetEventProcessor().PushEvent(ent); err != nil {
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
			if !isEtcdClientConnected(e.client) {
				e.reconnectAndRecover()
			}
		}
	}
}

func (e *EtcdDiscovery) connect() error {
	client, err := createEtcdClient(e.conf)
	if err != nil {
		return err
	}
	e.client = client
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
	// 旧 client 关闭连接
	if oldClient != nil {
		_ = oldClient.Close()
	}

	// 通知所有 watcher 重新启动
	e.watchers.Range(func(key, value any) bool {
		w := value.(*watcher)
		w.Restart()
		return true
	})
}

// 工具函数
func normalizeConf(conf *config.ClusterConf) *config.ClusterConf {
	if conf.DiscoveryConf == nil {
		conf.DiscoveryConf = &config.DiscoveryConf{
			Path:       defaultPath,
			TTL:        defaultTTL,
			MasterPath: defaultMasterKey,
		}
	}
	if conf.DiscoveryConf.TTL == 0 {
		conf.DiscoveryConf.TTL = defaultTTL
	}
	return conf
}

func createEtcdClient(conf *config.ClusterConf) (*clientv3.Client, error) {
	cfg := clientv3.Config{
		Endpoints:   conf.ETCDConf.Endpoints,
		DialTimeout: conf.ETCDConf.DialTimeout,
		Username:    conf.ETCDConf.UserName,
		Password:    conf.ETCDConf.Password,
	}

	var loggerCfg zap.Config
	if config.IsDebug() {
		loggerCfg = zap.NewDevelopmentConfig()
	} else {
		loggerCfg = zap.NewProductionConfig()
	}

	if conf.ETCDConf.NoLogger {
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
	// 获取当前活跃的连接
	conn := client.ActiveConnection()
	if conn == nil {
		return false
	}

	// 检查连接状态
	state := conn.GetState()
	return state == connectivity.Ready || state == connectivity.Idle
}

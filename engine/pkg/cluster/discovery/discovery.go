package discovery

import (
	"context"
	"go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
	"sync"
	"sync/atomic"

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
	conf     *config.ClusterConf
	client   *clientv3.Client
	ctx      context.Context
	cancel   context.CancelFunc
	started  atomic.Bool
	watchers sync.Map // map[string]*serviceWatcher

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
	e.conf = normalizeConf(conf)
	e.IEventProcessor = proc
	e.IEventHandler = event.NewHandler()
	e.IEventHandler.Init(proc)

	ctx, cancel := context.WithCancel(context.Background())
	e.ctx = ctx
	e.cancel = cancel

	client, err := createEtcdClient(e.conf)
	if err != nil {
		return err
	}
	e.client = client

	proc.RegEventReceiverFunc(event.SysEventServiceReg, e.IEventHandler, e.onRegister)
	proc.RegEventReceiverFunc(event.SysEventServiceDis, e.IEventHandler, e.onUnregister)

	return nil
}

func (e *EtcdDiscovery) Start() {
	if !e.started.CompareAndSwap(false, true) {
		return
	}
	go e.watchLoop()
	e.syncInitialState()
}

func (e *EtcdDiscovery) Close() {
	if !e.started.CompareAndSwap(true, false) {
		return
	}
	e.cancel()
	e.watchers.Range(func(_, v any) bool {
		v.(*serviceWatcher).Stop()
		return true
	})
	_ = e.client.Close()
}

func (e *EtcdDiscovery) syncInitialState() {
	resp, err := e.client.Get(context.Background(), e.conf.DiscoveryConf.Path, clientv3.WithPrefix())
	if err != nil {
		log.SysLogger.Errorf("sync services failed: %v", err)
		return
	}
	for _, kv := range resp.Kvs {
		ent := event.NewEvent()
		ent.Type = event.SysEventETCDPut
		ent.Data = kv
		if err = e.GetEventProcessor().PushEvent(ent); err != nil {
			ent.Release()
			log.SysLogger.Errorf("sync service error: %v", err)
		}
	}
}

func (e *EtcdDiscovery) onRegister(ev inf.IEvent) {
	ent := ev.(*event.Event)
	pid := ent.Data.(*actor.PID)
	if _, ok := e.watchers.Load(pid.ServiceUid); ok {
		return // already registered
	}
	w := newServiceWatcher(pid, e)
	e.watchers.Store(pid.ServiceUid, w)
	if err := w.Start(); err != nil {
		log.SysLogger.Errorf("start service watcher failed: %v", err)
	}
}

func (e *EtcdDiscovery) onUnregister(ev inf.IEvent) {
	ent := ev.(*event.Event)
	pid := ent.Data.(*actor.PID)
	if v, ok := e.watchers.LoadAndDelete(pid.ServiceUid); ok {
		v.(*serviceWatcher).Stop()
	}
}

func (e *EtcdDiscovery) watchLoop() {
	watchChan := e.client.Watch(e.ctx, e.conf.DiscoveryConf.Path, clientv3.WithPrefix())
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
				evType := event.SysEventETCDPut
				if ev.Type == clientv3.EventTypeDelete {
					evType = event.SysEventETCDDel
				}
				ent := event.NewEvent()
				ent.Type = int32(evType)
				ent.Data = ev.Kv
				if err := e.GetEventProcessor().PushEvent(ent); err != nil {
					log.SysLogger.Errorf("etcd event error: %v", err)
				}
			}
		}
	}
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
	loggerCfg := zap.NewProductionConfig()
	logger, err := loggerCfg.Build()
	if err != nil {
		return nil, err
	}
	cfg.Logger = logger
	return clientv3.New(cfg)
}

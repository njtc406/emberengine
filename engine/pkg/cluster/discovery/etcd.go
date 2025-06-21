// Package discovery
// @Title  服务发现
// @Description  基于ETCD的服务发现实现
// @Author  yr  2024/8/29 下午3:42
// @Update  yr  2024/8/29 下午3:42
package discovery

//const (
//	minWatchTTL       = 3
//	defaultPath       = "/ember/service"
//	defaultMasterPath = "/ember/master"
//)
//
//func init() {
//	Register("etcd", NewEtcdDiscovery())
//}
//
//// EtcdDiscovery 实现基于ETCD的服务发现
//type EtcdDiscovery struct {
//	inf.IEventProcessor
//	inf.IEventHandler
//
//	client   *clientv3.Client
//	conf     *config.ClusterConf
//	ctx      context.Context
//	cancel   context.CancelFunc
//	wg       sync.WaitGroup
//	running  atomic.Bool
//	watchers *watcherManager
//}
//
//// NewEtcdDiscovery 创建新的ETCD服务发现实例
//func NewEtcdDiscovery() *EtcdDiscovery {
//	return &EtcdDiscovery{}
//}
//
//func fixDiscoverConf(conf *config.ClusterConf) *config.ClusterConf {
//	if conf.DiscoveryConf == nil {
//		conf.DiscoveryConf = &config.DiscoveryConf{
//			Path:       defaultPath,
//			TTL:        minWatchTTL,
//			MasterPath: defaultMasterPath,
//		}
//	}
//
//	if conf.DiscoveryConf.TTL == 0 {
//		conf.DiscoveryConf.TTL = minWatchTTL
//	}
//	return conf
//}
//
//// Init 初始化服务发现
//func (d *EtcdDiscovery) Init(eventProcessor inf.IEventProcessor, conf *config.ClusterConf) error {
//	conf = fixDiscoverConf(conf)
//	d.conf = conf
//
//	if err := d.connect(); err != nil {
//		return fmt.Errorf("etcd connection failed: %w", err)
//	}
//
//	d.IEventProcessor = eventProcessor
//	d.IEventHandler = event.NewHandler()
//	d.IEventHandler.Init(eventProcessor)
//	ctx, cancel := context.WithCancel(context.Background())
//	d.cancel = cancel
//	d.ctx = ctx
//	d.watchers = newWatcherManager()
//
//	return nil
//}
//
//// connect 建立ETCD连接
//func (d *EtcdDiscovery) connect() error {
//	if len(d.conf.ETCDConf.Endpoints) == 0 {
//		// 允许不使用服务发现,所有调用服务都是本地服务
//		log.SysLogger.Info("etcd end points is empty")
//		return nil
//	}
//
//	var zapConfig zap.Config
//	if config.IsDebug() {
//		zapConfig = zap.NewDevelopmentConfig()
//	} else {
//		zapConfig = zap.NewProductionConfig()
//	}
//	logger, err := zapConfig.Build()
//	if err != nil {
//		return fmt.Errorf("failed to create etcd logger: %w", err)
//	}
//
//	cli, err := clientv3.New(clientv3.Config{
//		Endpoints:   d.conf.ETCDConf.Endpoints,
//		DialTimeout: d.conf.ETCDConf.DialTimeout,
//		Username:    d.conf.ETCDConf.UserName,
//		Password:    d.conf.ETCDConf.Password,
//		Logger:      logger,
//	})
//	if err != nil {
//		return fmt.Errorf("failed to create etcd client: %w", err)
//	}
//
//	d.client = cli
//	return nil
//}
//
//// Start 启动服务发现
//func (d *EtcdDiscovery) Start() {
//	if !d.running.CompareAndSwap(false, true) {
//		return
//	}
//
//	// 注册事件处理器
//	d.IEventProcessor.RegEventReceiverFunc(event.SysEventServiceReg, d.IEventHandler, d.handleServiceRegister)
//	d.IEventProcessor.RegEventReceiverFunc(event.SysEventServiceDis, d.IEventHandler, d.handleServiceUnregister)
//
//	if !d.isConnect() {
//		return
//	}
//
//	// 拉取当前已经注册的所有服务
//	d.syncServices()
//
//	// 后台监听服务
//	d.wg.Add(1)
//	go d.watchLoop()
//}
//
//// Close 关闭服务发现
//func (d *EtcdDiscovery) Close() {
//	if !d.running.CompareAndSwap(true, false) {
//		return
//	}
//
//	d.cancel()
//	d.wg.Wait()
//
//	if !d.isConnect() {
//		return
//	}
//
//	// 先回收watcher的所有key
//	d.watchers.CloseAll(func(w *watcher) {
//		w.Stop()
//	})
//
//	// 再关闭etcd链接
//	if err := d.client.Close(); err != nil {
//		log.SysLogger.Errorf("etcd client close error: %v", err)
//	}
//	return
//}
//
//func (d *EtcdDiscovery) isConnect() bool {
//	return d.client != nil
//}
//
//// watchLoop 持续监听ETCD变更
//func (d *EtcdDiscovery) watchLoop() {
//	defer func() {
//		if err := recover(); err != nil {
//			log.SysLogger.Errorf("etcd watch loop panic: %v", err)
//		}
//	}()
//	defer d.wg.Done()
//
//	ctx, cancel := context.WithCancel(context.Background())
//	changes := d.watchKey(ctx, d.conf.DiscoveryConf.Path, clientv3.WithPrefix())
//	defer cancel()
//	var backoff uint32 = 50      // 初始退避
//	var maxBackoff uint32 = 1600 // 最大退避时间(最大退避5次)
//	for {
//		select {
//		case <-d.ctx.Done():
//			return
//		case resp, ok := <-changes:
//			if !ok {
//				log.SysLogger.Errorf("etcd watch channel closed")
//				return
//			}
//			if resp.Canceled {
//				log.SysLogger.Errorf("etcd watch canceled")
//				return
//			}
//			if resp.Err() != nil {
//				log.SysLogger.Errorf("etcd watch error: %v", resp.Err())
//
//				// 使用指数退避,防止网络波动的时候连接不到etcd导致这里一直占用资源
//				if backoff < maxBackoff {
//					backoff *= 2
//				}
//				time.Sleep(time.Millisecond * time.Duration(backoff))
//				continue
//			}
//
//			d.processWatchEvent(resp.Events)
//		}
//	}
//}
//
//// processWatchEvent 处理单个watch事件
//func (d *EtcdDiscovery) processWatchEvent(evs []*clientv3.Event) {
//	for _, ev := range evs {
//		ent := event.NewEvent()
//		data := *ev.Kv
//		switch ev.Type {
//		case clientv3.EventTypePut:
//			ent.Type = event.SysEventETCDPut
//			ent.Data = &data
//		case clientv3.EventTypeDelete:
//			ent.Type = event.SysEventETCDDel
//			ent.Data = &data
//		default:
//			ent.Release()
//			continue
//		}
//
//		// 异步处理事件,事件会在处理完之后自动释放
//		if err := d.PushEvent(ent); err != nil {
//			ent.Release()
//			log.SysLogger.Errorf("process watch event[%d] data[%s] error: %v", ev.Type, data.String(), err)
//		}
//	}
//}
//
//// syncServices 同步当前所有服务
//func (d *EtcdDiscovery) syncServices() {
//	resp, err := d.client.Get(context.Background(), d.conf.DiscoveryConf.Path, clientv3.WithPrefix())
//	if err != nil {
//		log.SysLogger.Errorf("get existing services error: %v", err)
//		return
//	}
//
//	for _, kv := range resp.Kvs {
//		data := *kv
//		ent := event.NewEvent()
//		ent.Type = event.SysEventETCDPut
//		ent.Data = &data
//		if err := d.PushEvent(ent); err != nil {
//			ent.Release()
//			log.SysLogger.Errorf("sync service error: %v", err)
//		}
//	}
//}
//
//// handleServiceRegister 处理服务注册事件
//func (d *EtcdDiscovery) handleServiceRegister(ev inf.IEvent) {
//	if !d.isConnect() {
//		return
//	}
//
//	ent, ok := ev.(*event.Event)
//	if !ok || ent.Type != event.SysEventServiceReg {
//		return
//	}
//
//	pid, ok := ent.Data.(*actor.PID)
//	if !ok {
//		log.SysLogger.Error("invalid service registration data")
//		return
//	}
//
//	watcher := d.watchers.Get(pid.ServiceUid) // 如果是主从模式,那么不应该放在同一个node上
//	if watcher != nil {
//		log.SysLogger.Warnf("service %s already registered", pid.ServiceUid)
//		return
//	}
//
//	watcher = newWatcher(pid, d)
//	if err := watcher.Start(); err != nil {
//		log.SysLogger.Errorf("start service watcher failed: %v", err)
//		return
//	}
//	d.watchers.Add(pid.ServiceUid, watcher)
//
//	// 开启了主从,才会选举,否则直接设置为主服务
//	if len(ent.AnyExt) > 0 && ent.AnyExt[0].(bool) {
//		watcher.electMaster()
//	} else {
//		watcher.isMaster.Store(true)
//		watcher.pid.SetMaster(true)
//	}
//
//	// 注册到etcd
//	if err := watcher.registerService(); err != nil {
//		log.SysLogger.Errorf("register service[%s] failed: %v", pid.String(), err)
//	}
//}
//
//// handleServiceUnregister 处理服务注销事件
//func (d *EtcdDiscovery) handleServiceUnregister(ev inf.IEvent) {
//	if !d.isConnect() {
//		return
//	}
//
//	ent, ok := ev.(*event.Event)
//	if !ok || ent.Type != event.SysEventServiceDis {
//		return
//	}
//
//	pid, ok := ent.Data.(*actor.PID)
//	if !ok {
//		log.SysLogger.Error("invalid service unregistration data")
//		return
//	}
//
//	if watcher := d.watchers.Remove(pid.ServiceUid); watcher != nil {
//		watcher.Stop()
//	}
//}
//
//func (d *EtcdDiscovery) watchKey(ctx context.Context, key string, options ...clientv3.OpOption) <-chan clientv3.WatchResponse {
//	return d.client.Watch(ctx, key, options...)
//}
//
//// watcher 管理单个服务的生命周期
//type watcher struct {
//	pid         *actor.PID
//	discovery   *EtcdDiscovery
//	leaseID     clientv3.LeaseID
//	ctx         context.Context
//	cancel      context.CancelFunc
//	wg          sync.WaitGroup
//	watchMasterCtx    context.Context
//	watchMasterCancel context.CancelFunc
//	watchMasterWg     sync.WaitGroup
//	isMaster    atomic.Bool
//	closed      atomic.Bool
//}
//
//func newWatcher(pid *actor.PID, d *EtcdDiscovery) *watcher {
//	ctx, cancel := context.WithCancel(context.Background())
//	return &watcher{
//		pid:       pid,
//		discovery: d,
//		ctx:       ctx,
//		cancel:    cancel,
//	}
//}
//
//func (w *watcher) Start() error {
//	if w.closed.Load() {
//		return nil
//	}
//	// 初始化租约
//	if err := w.initLease(); err != nil {
//		return fmt.Errorf("init lease failed: %w", err)
//	}
//
//	// 启动保活协程
//	w.wg.Add(1)
//	go w.keepaliveLoop()
//
//	return nil
//}
//
//func (w *watcher) Stop() {
//	if w.closed.CompareAndSwap(false, true) {
//		w.stopWatchMaster()
//		// 释放租约,会自动删除关联的rpckey和masterkey
//		w.releaseLease()
//		w.cancel()
//		w.wg.Wait()
//	}
//}
//
//func (w *watcher) IsMaster() bool {
//	return w.isMaster.Load()
//}
//
//func (w *watcher) releaseLease() {
//	if w.leaseID != 0 {
//		_, _ = w.discovery.client.Revoke(context.Background(), w.leaseID)
//		w.leaseID = 0
//	}
//}
//
//func (w *watcher) initLease() error {
//	resp, err := w.discovery.client.Grant(context.Background(), w.discovery.conf.DiscoveryConf.TTL)
//	if err != nil {
//		return fmt.Errorf("create lease failed: %w", err)
//	}
//	w.releaseLease() // 等新的租约申请成功,再释放老的租约,防止key抖动
//	w.leaseID = resp.ID
//	return nil
//}
//
//func (w *watcher) keepaliveLoop() {
//	defer func() {
//		if err := recover(); err != nil {
//			log.SysLogger.Errorf("etcd keepalive error: %v", err)
//		}
//		w.wg.Done()
//	}()
//	for {
//		select {
//		case <-w.ctx.Done():
//			return
//		default:
//			w.keepalive() // 正常情况下这里会阻塞
//
//			// 如果保活失败,重新申请租约,尝试重连
//			if err := w.initLease(); err != nil {
//				log.SysLogger.Errorf("init lease error: %v", err)
//				continue
//			}
//
//			// 这里给一个重连的间隔时间,防止网络波动的时候瞬间有大量链接需要重连
//			interval := util.RandN(5)
//			if interval > 0 {
//				time.Sleep(time.Duration(interval) * time.Second)
//			}
//		}
//	}
//}
//
//func (w *watcher) keepalive() {
//	log.SysLogger.Debug("discovery watcher keepalive start")
//	defer func() {
//		if err := recover(); err != nil {
//			log.SysLogger.Errorf("etcd keepalive error: %v", err)
//		}
//	}()
//	if w.leaseID <= 0 {
//		// 这里可能是由于服务下线,导致已经删除了
//		return
//	}
//	kaRespCh, err := w.discovery.client.KeepAlive(w.ctx, w.leaseID)
//	if err != nil {
//		log.SysLogger.Errorf("etcd keepalive error: %v", err)
//		return
//	}
//
//	for {
//		select {
//		case <-w.ctx.Done():
//			// 实际的退出
//			return
//		case kaResp, ok := <-kaRespCh:
//			if !ok || kaResp == nil {
//				log.SysLogger.Errorf("etcd keepalive error: %v", kaResp)
//				// 退出当前保活,尝试重连
//				return
//			}
//		}
//	}
//}
//
//// electMaster 选举主节点
//func (w *watcher) electMaster() {
//	masterKey := path.Join(
//		w.discovery.conf.DiscoveryConf.MasterPath,
//		w.pid.GetServiceUid(),
//	)
//
//	// 先停止监听主节点变化
//	w.stopWatchMaster()
//
//	// 先设置为从节点
//	w.isMaster.Store(false)
//	w.pid.SetMaster(false)
//
//	// TODO 目前暂时使用直接抢占的方式,后续有其他需求再做成策略
//	// TODO etcd/concurrency后续使用这个来优化
//	// 尝试成为主节点
//	txnResp, err := w.discovery.client.Txn(context.Background()).
//		If(clientv3.Compare(clientv3.CreateRevision(masterKey), "=", 0)).
//		Then(clientv3.OpPut(masterKey, w.pid.GetServiceUid(), clientv3.WithLease(w.leaseID))).
//		Commit()
//
//	if err != nil {
//		log.SysLogger.Errorf("master election transaction failed: %v", err)
//		goto SLAVE
//	}
//
//	if txnResp.Succeeded {
//		// 设置为主节点
//		w.isMaster.Store(true)
//		w.pid.SetMaster(true)
//
//		// 主节点产生事件
//
//		// 更新数据
//		if err := w.registerService(); err != nil {
//			log.SysLogger.Errorf("register service[%s] failed: %v", w.pid.String(), err)
//		} else {
//			// TODO 同时注册slave消息监听
//
//			// TODO 通知服务身份转变
//
//			log.SysLogger.Debugf("master election success,leaseId:%d pid:%s", w.leaseID, w.pid.String())
//		}
//
//		return
//	}
//
//SLAVE:
//	// 从节点监听主节点变化
//	w.watchMasterWg.Add(1)
//	go w.startWatchMaster(masterKey)
//}
//
//func (w *watcher) registerService() error {
//	servicePath := path.Join(w.discovery.conf.DiscoveryConf.Path, w.pid.GetServiceUid())
//	pidData, err := protojson.Marshal(w.pid)
//	if err != nil {
//		return fmt.Errorf("marshal pid failed: %w", err)
//	}
//
//	_, err = w.discovery.client.Put(context.Background(), servicePath, string(pidData), clientv3.WithLease(w.leaseID))
//	if err != nil {
//		return fmt.Errorf("register service failed: %w", err)
//	}
//
//	return err
//}
//
//// startWatchMaster 监听主节点变化
//func (w *watcher) startWatchMaster(masterKey string) {
//	defer w.watchMasterWg.Done()
//	ctx, cancel := context.WithCancel(context.Background())
//	w.watchMasterCtx = ctx
//	w.watchMasterCancel = cancel
//	defer func() {
//		w.watchMasterCtx = nil
//		w.watchMasterCancel = nil
//	}()
//	watchChan := w.discovery.watchKey(ctx, masterKey)
//	log.SysLogger.Infof("watching master key: %s leaseId:%d", masterKey, w.leaseID)
//
//	for {
//		select {
//		case <-w.ctx.Done():
//			return
//		case <-w.watchMasterCtx.Done():
//			return
//		case resp := <-watchChan:
//			if resp.Err() != nil {
//				log.SysLogger.Errorf("watch master error: %v", resp.Err())
//				go w.electMaster() // 在新的协程中执行,防止死锁
//				return
//			}
//
//			for _, ev := range resp.Events {
//				if ev.Type == clientv3.EventTypeDelete { // 只处理主节点离线事件
//					log.SysLogger.Info("master node lost, triggering re-election")
//					go w.electMaster() // 在新的协程中执行,防止死锁
//					return
//				}
//			}
//		}
//	}
//}
//
//func (w *watcher) stopWatchMaster() {
//	if w.watchMasterCancel != nil {
//		w.watchMasterCancel()
//		w.watchMasterWg.Wait()
//	}
//}
//
//// watcherManager 管理所有服务watcher
//type watcherManager struct {
//	watchers sync.Map
//}
//
//func newWatcherManager() *watcherManager {
//	return &watcherManager{}
//}
//
//func (m *watcherManager) Add(key string, watcher *watcher) {
//	m.watchers.Store(key, watcher)
//}
//
//func (m *watcherManager) Get(key string) *watcher {
//	v, ok := m.watchers.Load(key)
//	if !ok {
//		return nil
//	}
//	return v.(*watcher)
//}
//
//func (m *watcherManager) Remove(key string) *watcher {
//	v, ok := m.watchers.LoadAndDelete(key)
//	if !ok {
//		return nil
//	}
//	return v.(*watcher)
//}
//
//func (m *watcherManager) CloseAll(f func(w *watcher)) {
//	m.watchers.Range(func(key, value interface{}) bool {
//		if watcher, ok := value.(*watcher); ok {
//			f(watcher)
//		}
//		return true
//	})
//}

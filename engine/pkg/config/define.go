package config

import (
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/njtc406/viper"
	"time"
)

// TODO 这是第一版,后续可能会根据需求改进配置

const (
	Debug   = `debug`
	Release = `release`
)

type conf struct {
	NodeConf     *NodeConf       `binding:"required"` // 节点基础配置
	SystemLogger *log.LoggerConf `binding:"required"` // 系统日志
	ClusterConf  *ClusterConf    `binding:"required"` // 集群配置
	ServiceConf  *ServiceConf    `binding:"required"` // 服务配置
}

type NodeConf struct {
	NodeId            string        `binding:""`         // 节点ID(目前这个没用,节点id是节点启动的时候自动生成的)
	SystemStatus      string        `binding:"required"` // 系统状态(debug/release)
	PVCPath           string        `binding:"required"` // 数据持久化目录(默认./data)
	PVPath            string        `binding:"required"` // 缓存目录(默认./run)
	ProfilerInterval  time.Duration `binding:""`         // 性能分析间隔(默认0,不开启)
	AntsPoolSize      int           `binding:"required"` // 线程池大小
	MonitorTimerSize  int           `binding:""`         // 定时器数量(用于监控rpc调用的timer)(默认10000)
	MonitorBucketSize int           `binding:""`         // 定时器桶数量(默认20)
	EventBusConf      *EventBusConf `binding:""`         // nats配置
}

type ClusterConf struct {
	ETCDConf       *ETCDConf          `binding:"required"` // etcd配置
	RPCServers     []*RPCServer       `binding:""`         // rpc服务配置
	DiscoveryType  string             `binding:""`         // 服务发现类型(默认etcd)
	RemoteConfPath string             `binding:""`         // 远程配置路径(开启了远程配置才会使用,且必须配置etcd)(暂未使用)
	DiscoveryConf  *EtcdDiscoveryConf `binding:""`         // 服务发现配置(目前先直接配置,后续会支持多种服务发现方式)
}

type ServiceConf struct {
	OpenRemote      bool                      `binding:""`         // 是否开启远程配置(默认使用本地)
	RemoteConfPath  string                    `binding:""`         // 远程配置路径(开启了远程配置才会使用,且必须配置etcd)
	StartServices   []*ServiceInitConf        `binding:"required"` // 启动服务列表(按照配置的顺序启动!!)
	ServicesConfMap map[string]*ServiceConfig `binding:"required"` // 服务配置
}

type ETCDConf struct {
	Endpoints   []string
	DialTimeout time.Duration // 默认3秒
	UserName    string
	Password    string
}

type RPCServer struct {
	Addr    string // rpc监听地址
	Protoc  string // 协议
	Type    string // 服务类型(默认grpc)
	Cert    string `binding:""` // 证书
	CertKey string `binding:""` // 证书密钥
	CAs     string `binding:""` // ca证书
}

type ServiceInitConf struct {
	ServiceId   string      `binding:""`         // 服务唯一id(如果是全局唯一的服务,且不会启动多个,那么可以为空)
	ServiceName string      `binding:"required"` // 服务名称
	Type        string      `binding:"required"` // 服务类型
	Version     int64       `binding:""`         // 服务版本
	ServerId    int32       `binding:"required"` // 服务ID
	TimerConf   *TimerConf  `binding:""`         // 定时器配置
	RpcType     string      `binding:""`         // 远程调用方式(默认使用rpcx)
	WorkerConf  *WorkerConf `binding:""`         // 工作线程配置
}

type ServiceConfig struct {
	ServiceName   string             // 服务名称
	ConfName      string             // 配置文件名称
	ConfPath      string             // 配置文件路径
	ConfType      string             // 配置文件类型
	CfgCreator    func() interface{} // 配置获取器(获取真实的配置格式)
	Cfg           interface{}        // 配置结构体(解析后的配置)
	DefaultSetFun func(*viper.Viper) // 默认配置函数
	OnChangeFun   func()             // 配置变化处理函数
}

type EtcdDiscoveryConf struct {
	Path string // rpc注册路径
	TTL  int64  // 证书有效期(默认3秒)
}

type TimerConf struct {
	TimerSize       int `binding:""` // 定时器数量
	TimerBucketSize int `binding:""` // 定时器调度器存储桶数量(减少锁的冲突,增加并发)
}

type WorkerConf struct {
	UserMailboxSize      int  `binding:""` // 默认1024(最终值都是2的n次方,不足时向上取到最近的2的n次方)
	SystemMailboxSize    int  `binding:""` // 默认16(最终值都是2的n次方,不足时向上取到最近的2的n次方)
	WorkerNum            int  `binding:""` // 工作线程数量(默认1,如果大于1则启动多线程模式,需要自行控制资源)
	DynamicWorkerScaling bool `binding:""` // 动态线程池扩展(默认false),如果开启则根据负载情况动态扩展线程池(请确保需要单线程的服务不开启这个标记)
	VirtualWorkerRate    int  `binding:""` // 虚拟线程倍率(默认10)(当workerNum大于1时,虚拟线程倍率用来控制虚拟线程的数量 哈希环上的节点数量=workernum*rate)
}

type EventBusConf struct {
	GlobalPrefix string    `binding:""` // 全局事件前缀
	ServerPrefix string    `binding:""` // 服务事件前缀
	NodePrefix   string    `binding:""` // 节点事件前缀
	ShardCount   int       `binding:""` // 分段锁数量
	NatsConf     *NatsConf `binding:""` // nats配置
}

type NatsConf struct {
	EndPoints          []string      `binding:""` // nats地址
	UserName           string        `binding:""` // nats用户名
	Password           string        `binding:""` // nats密码
	Token              string        `binding:""` // nats token
	Secure             string        `binding:""` // nats secure
	Cert               string        `binding:""` // 证书
	CertKey            string        `binding:""` // 证书密钥
	CAs                string        `binding:""` // ca证书
	MaxReconnects      int           `binding:""` // 最大重连次数
	ReconnectWait      time.Duration `binding:""` // 重连间隔
	Timeout            time.Duration `binding:""` // 连接超时时间
	PingInterval       time.Duration `binding:""` // ping间隔时间
	PingMaxOutstanding int           `binding:""` // 最大未响应ping数
	ReconnectBufSize   int           `binding:""` // 重连缓冲区大小
}

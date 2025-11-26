package config

import (
	"time"

	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/viper"
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
	NodeId            string            `binding:""`         // 节点ID(目前这个没用,节点id是节点启动的时候自动生成的)
	SystemStatus      string            `binding:"required"` // 系统状态(debug/release)
	PVCPath           string            `binding:"required"` // 数据持久化目录(默认./data)
	PVPath            string            `binding:"required"` // 缓存目录(默认./run)
	ProfilerInterval  time.Duration     `binding:""`         // 性能分析间隔(默认0,不开启)
	AntsPoolSize      int               `binding:"required"` // 线程池大小
	MonitorTimerSize  int               `binding:""`         // 定时器数量(用于监控rpc调用的timer)(默认10000)
	MonitorBucketSize int               `binding:""`         // 定时器桶数量(默认20)
	EventBusConf      *EventBusConf     `binding:""`         // nats配置
	DeDuplicatorConf  *DeDuplicatorConf `binding:""`         // deDuplicator配置
}

type ClusterConf struct {
	ETCDConf       *ETCDConf      `binding:"required"` // etcd配置
	RPCServers     []*RPCServer   `binding:""`         // rpc服务配置
	DiscoveryType  string         `binding:""`         // 服务发现类型(默认etcd)
	RemoteConfPath string         `binding:""`         // 远程配置路径(开启了远程配置才会使用,且必须配置etcd)(暂未使用)
	DiscoveryConf  *DiscoveryConf `binding:""`         // 服务发现配置(目前先直接配置,后续会支持多种服务发现方式)
}

type ServiceConf struct {
	OpenRemote      bool                      `binding:""`         // 是否开启远程配置(默认使用本地)
	RemoteConfPath  string                    `binding:""`         // 远程配置路径(开启了远程配置才会使用,且必须配置etcd)
	StartServices   []*ServiceInitConf        `binding:"required"` // 启动服务列表(按照配置的顺序启动!!)
	ServicesConfMap map[string]*ServiceConfig `binding:"required"` // 服务配置 [服务名称]配置
}

type ETCDConf struct {
	Endpoints   []string
	DialTimeout time.Duration // 默认3秒
	UserName    string
	Password    string
	NoLogger    bool `binding:""` // 是否不使用日志
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
	ClassName              string          `binding:"required"` // 服务类名
	ServiceId              string          `binding:""`         // 服务唯一id(如果是全局唯一的服务,且不会启动多个,那么可以为空)
	ServiceName            string          `binding:""`         // 服务名称(调用时使用这个名字)
	Type                   string          `binding:"required"` // 服务类型
	Version                int64           `binding:""`         // 服务版本
	ServerId               int32           `binding:"required"` // 服务ID
	TimerConf              *TimerConf      `binding:""`         // 定时器配置
	RpcType                string          `binding:""`         // 远程调用方式(默认使用rpcx)
	Mailbox                *MailboxConf    `binding:""`         // 邮箱配置
	LogConf                *ServiceLogConf `binding:""`         // 日志配置
	IsPrimarySecondaryMode bool            `binding:""`         // 是否是主从模式(默认不开启)
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

type DiscoveryConf struct {
	Path       string // rpc注册路径
	TTL        int64  // 证书有效期(默认3秒)
	MasterPath string // 主从选举路径
}

type TimerConf struct {
	TimerSize       int `binding:""` // 定时器数量
	TimerBucketSize int `binding:""` // 定时器调度器存储桶数量(减少锁的冲突,增加并发)
}

type MailboxConf struct {
	MailboxType    string                `binding:""` // 邮箱类型(single/concurrent,默认single)
	SchedulePolicy *WorkerSchedulePolicy `binding:""` // 工作线程调度策略
}

type EventBusConf struct {
	GlobalPrefix string    `binding:""` // 全局事件前缀
	ServerPrefix string    `binding:""` // 服务事件前缀
	MasterPrefix string    `binding:""` // 主服务事件前缀(用于主从同步)
	SlavePrefix  string    `binding:""` // 从服务事件前缀(用于主从同步)
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

type ServiceLogConf struct {
	Enable bool            `binding:""` // 是否开启独立logger
	Config *log.LoggerConf // 日志配置
}

type WorkerStrategyConfig struct {
	Name           string                  `binding:""` // 策略名称
	Params         map[string]interface{}  `binding:""` // 策略参数,如果是复合策略,需要固定给一个map["mode"]="all/any"
	MinWorkerNum   int                     `binding:""` // 最小工作线程数量(只有开启了动态worker扩展,这个值才会生效)
	MaxWorkerNum   int                     `binding:""` // 最大工作线程数量(只有开启了动态worker扩展,这个值才会生效)
	GrowthFactor   float64                 `binding:""` // 扩容因子(线程池的数量=当前线程池数量*扩容因子)
	ShrinkFactor   float64                 `binding:""` // 缩容因子(线程池的数量=当前线程池数量*缩容因子)
	ResizeCoolDown time.Duration           `binding:""` // 缩容冷却时间(默认1秒)(当负载小于最小负载时,则关闭多余的线程)
	Subs           []*WorkerStrategyConfig `binding:""` // 子策略，复合策略才有
}

type MultiLevelWorkerConf struct {
	WaitMode        string                           `binding:""` // 等待模式: busy / cond (默认busy)
	Strategy        def.ScheduleStrategy             `binding:""` // 调度策略: absolute / weighted / fair (默认absolute)
	TotalBatchLimit int                              `binding:""` // 总批次上限(默认128)
	PriorityBatches map[def.Priority]*PriorityConfig `binding:""` // 各优先级批量大小 {"sys":32, "urgent":16, "high":12, "normal":8}
}

// PriorityConfig 单个优先级配置
type PriorityConfig struct {
	BatchSize int `json:"batch_size"` // 该级别批量处理大小
	Weight    int `json:"weight"`     // 调度权重（用于加权策略）
}

type WorkerIdlerConf struct {
	EnableCond           bool          `binding:""` // 是否开启条件等待(默认false)
	BackoffBaseDelay     time.Duration `binding:""` // 退避基础时间
	BackoffMaxDelay      time.Duration `binding:""` // 最大退避时间
	BackoffMaxRetries    int           `binding:""` // 最大退避时间扩大次数
	MaxIdleBeforeBackoff int           `binding:""` // 最大空闲次数
}

type DeDuplicatorConf struct {
	DeDuplicatorType     string        `binding:""` // 去重处理器类型(ttl和lru)
	DeDuplicatorTTL      time.Duration `binding:""`
	DeDuplicatorCleanTTL time.Duration `binding:""`
	DeDuplicatorSize     int           `binding:""`
}

type WorkerSchedulePolicy struct {
	InitialWorkerNum  int                   `binding:""` // 初始工作线程数量(默认1)
	VirtualWorkerRate int                   `binding:""` // 虚拟线程率(默认100)
	EnableAutoScaling bool                  `binding:""` // 是否开启自动扩展(默认false)
	ScalingStrategy   *WorkerStrategyConfig `binding:""` // 自动扩展策略配置
	IdlerConf         *WorkerIdlerConf      `binding:""` // 工作线程执行配置
	MultiLevelConf    *MultiLevelWorkerConf `binding:""` // 多优先级邮箱配置(仅当concurrency时生效)
}

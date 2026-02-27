package config

import (
	"encoding/json"
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

func (c *conf) String() string {
	jsonStr, _ := json.MarshalIndent(c, "", "  ")
	return string(jsonStr)
}

type NodeConf struct {
	NodeId           string            `binding:"required"` // 节点ID(目前这个只用于记录pid文件是组成生成路径)
	NodeType         string            `binding:"required"` // 节点类型(默认ember)
	SystemStatus     string            `binding:"required"` // 系统状态(debug/release)
	PVCPath          string            `binding:"required"` // 数据持久化目录(默认./data)
	PVPath           string            `binding:"required"` // 缓存目录(默认./run)
	AntsPoolSize     int               `binding:"required"` // 线程池大小
	RpcMonitorConf   *RpcMonitorConf   `binding:""`         // rpc监控配置
	EventBusConf     *EventBusConf     `binding:""`         // nats配置
	DeDuplicatorConf *DeDuplicatorConf `binding:""`         // deDuplicator配置
	TimingWheelConf  *TimingWheelConf  `binding:""`         // 定时器配置
	BusPoolSize      int               `binding:""`         // 消息总线缓存池池大小(默认10000)
}
type TimingWheelConf struct {
	Interval  time.Duration `binding:""` // 定时器间隔(默认10毫秒)
	WheelSize int64         `binding:""` // 定时器轮数(默认1000)
}

// rpc监控配置
type RpcMonitorConf struct {
	MonitorTimerSize  int `binding:""` // 定时器数量(用于监控rpc调用的timer)(默认10000)
	MonitorBucketSize int `binding:""` // 定时器桶数量(默认20)

	// WaitBucketCount: 等待表分桶数量（用于降低锁竞争）。
	// 建议为 2 的幂；若不是 2 的幂，运行时会向上取整到 2 的幂。
	// 默认 256。
	WaitBucketCount int `binding:""`
	// WaitBucketInitCap: 每个桶内部 map 的初始容量。
	// 配置 <=0 表示自动推导（基于 MonitorTimerSize / WaitBucketCount，且最小为 16）。
	WaitBucketInitCap int `binding:""`

	// RPC 超时配置
	DefaultRpcTimeout    time.Duration `binding:""` // RPC 调用默认超时时间(默认1秒)
	CheckTimeoutInterval time.Duration `binding:""` // RPC 超时检查间隔(默认1秒)
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

	// 网络层超时配置（仅 TCP 类型生效）
	ReadDeadline  time.Duration `binding:""` // 读超时(默认30秒)
	WriteDeadline time.Duration `binding:""` // 写超时(默认30秒)
}

type ServiceInitConf struct {
	ClassName   string     `binding:"required"` // 服务类名
	ServiceId   string     `binding:""`         // 服务唯一id(如果是全局唯一的服务,且不会启动多个,那么可以为空)
	ServiceName string     `binding:""`         // 服务名称(调用时使用这个名字)
	Type        string     `binding:"required"` // 服务类型
	Version     int64      `binding:""`         // 服务版本
	Partition   int32      `binding:"required"` // 服务分区ID(用于服务路由隔离)
	TimerConf   *TimerConf `binding:""`         // 定时器配置
	// StopGraceTimeout 已废弃：请使用 StopPolicy.GraceTimeout。
	// 保留该字段是为了兼容旧配置。
	StopGraceTimeout time.Duration `binding:""` // 关闭时等待窗口(默认0,不等待; 仅用于等待回调/队列自然收敛)
	// StopPolicy 停机策略配置：优雅窗口与停机时队列处理方式。
	StopPolicy             *StopPolicyConf `binding:""`
	RpcType                string          `binding:""` // 远程调用方式(默认使用nats)
	Mailbox                *MailboxConf    `binding:""` // 邮箱配置
	LogConf                *ServiceLogConf `binding:""` // 日志配置
	IsPrimarySecondaryMode bool            `binding:""` // 是否是主从模式(默认不开启)
	EventChanSize          int             `binding:""` // 事件通道大小(默认100)
}

// StopPolicyConf 服务停机策略
// GraceTimeout: 优雅窗口（期间 mailbox 挂起，仅放行必要消息）
// DrainPolicy: 停机时对队列剩余消息的处理方式
//   - "execute" : 继续执行剩余消息（默认，保持现有语义）
//   - "discard" : 丢弃剩余消息（仅回收/OnComplete，不执行业务）
type StopPolicyConf struct {
	GraceTimeout time.Duration `binding:""`
	DrainPolicy  string        `binding:""`
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

	// 故障恢复配置
	RecoveryConf *DiscoveryRecoveryConf `binding:""` // 故障恢复配置
}

// DiscoveryRecoveryConf 服务发现故障恢复配置
// 用于配置 watcher 重连和 keepalive 的退避策略
type DiscoveryRecoveryConf struct {
	// 退避策略基础延迟（默认1秒）
	BackoffBaseDelay time.Duration `binding:""`
	// 退避策略最大延迟（默认30秒）
	BackoffMaxDelay time.Duration `binding:""`
	// 详细日志输出的前N次重试（默认5次，前5次每次都打印日志）
	VerboseLogCount int `binding:""`
	// 日志输出间隔（默认10，之后每10次打印一次）
	LogInterval int `binding:""`
}

type TimerConf struct {
	TimerSize       int `binding:""` // 定时器数量
	TimerBucketSize int `binding:""` // 定时器调度器存储桶数量(减少锁的冲突,增加并发)
}

// MailboxConf 邮箱配置
// 职责：配置消息队列的队列模式和调度策略
type MailboxConf struct {
	// QueueMode 队列模式
	// - "dual":   双队列模式（系统队列+用户队列，适用于简单场景）
	// - "priority": 多优先级队列模式（支持多个优先级队列和调度策略）
	// 默认: "dual"
	QueueMode string `binding:""`

	// SchedulePolicy 调度策略配置（包含Worker数量、扩缩容、空闲控制等）
	SchedulePolicy *WorkerSchedulePolicy `binding:""`

	// MiddlewareConf 中间件配置
	MiddlewareConf *MailboxMiddlewareConf `binding:""`

	// EnableRWMode 启用 Mailbox 级读写分离
	// 开启后，标记为 ReadOnly 的 RPC 方法可跨 Worker 并发执行（RLock），
	// 写操作全 Service 独占串行（WLock）。
	// 默认: false（关闭，与当前完全一致）
	EnableRWMode bool `binding:""`

	// MaxConcurrentReads 整个 Service 最大并发读执行数（硬上限）
	// 仅在 EnableRWMode=true 时生效。
	// 默认: min(runtime.NumCPU()*4, 64)
	// 0 表示使用默认值，显式设为负数也回退到默认值。
	MaxConcurrentReads int `binding:""`

	// StopTimeout RW 模式下 Worker Stop 的最大等待时间
	// 超时后 Worker 放弃等待泄漏的读 goroutine，强制继续 Drain 并退出。
	// 仅在 EnableRWMode=true 时生效。
	// 默认: 10s
	StopTimeout time.Duration `binding:""`

	// MaxJobExecutionTime 单个 Job 的最大执行时长
	// 超过此阈值会打印 Warn 日志（watchdog 告警），不会中断执行。
	// 仅在 EnableRWMode=true 时生效。
	// 0 表示不启用 watchdog。
	// 默认: 30s
	MaxJobExecutionTime time.Duration `binding:""`
}

// MailboxMiddlewareConf 邮箱中间件配置
type MailboxMiddlewareConf struct {
	// EnableDispatchKeyStats 是否启用 DispatchKey 统计中间件（仅 debug 模式生效）
	// 用于统计热点 dispatcherKey，帮助发现消息分布不均问题
	// 默认: true（debug 模式下）
	EnableDispatchKeyStats bool `binding:""`

	// DispatchKeyStatsInterval 统计输出间隔
	// 默认: 10s
	DispatchKeyStatsInterval time.Duration `binding:""`

	// DispatchKeyStatsTopN 统计输出 TopN
	// 默认: 10
	DispatchKeyStatsTopN int `binding:""`

	// RateLimitConf 限流中间件配置（nil 表示不启用）
	RateLimitConf *RateLimitConf `binding:""`

	// CircuitBreakerConf 熔断中间件配置（nil 表示不启用）
	CircuitBreakerConf *CircuitBreakerConf `binding:""`
}

// RateLimitConf 限流中间件配置
type RateLimitConf struct {
	// Enable 是否启用限流
	Enable bool `binding:""`

	// Rate 每秒允许的请求数
	// 默认: 10000
	Rate float64 `binding:""`

	// Burst 突发流量上限（令牌桶容量）
	// 默认: 1000
	Burst int `binding:""`

	// SkipUrgent 是否跳过紧急及以上优先级的消息
	// 默认: true
	SkipUrgent bool `binding:""`
}

// CircuitBreakerConf 熔断中间件配置
type CircuitBreakerConf struct {
	// Enable 是否启用熔断
	Enable bool `binding:""`

	// FailureThreshold 触发熔断的连续失败次数
	// 默认: 5
	FailureThreshold int `binding:""`

	// SuccessThreshold 半开状态下恢复的成功次数
	// 默认: 3
	SuccessThreshold int `binding:""`

	// CooldownDuration 熔断冷却时间
	// 默认: 30s
	CooldownDuration time.Duration `binding:""`

	// WindowDuration 统计窗口时间
	// 默认: 60s
	WindowDuration time.Duration `binding:""`

	// HalfOpenMaxAllowed 半开状态允许的最大探测请求数
	// 默认: 3
	HalfOpenMaxAllowed int `binding:""`
}

type EventBusConf struct {
	GlobalPrefix   string    `binding:""` // 全局事件前缀
	ServerPrefix   string    `binding:""` // 服务事件前缀
	SpecificPrefix string    `binding:""` // 特定事件前缀(用于特定事件)
	MasterPrefix   string    `binding:""` // 主服务事件前缀(用于主从同步)
	SlavePrefix    string    `binding:""` // 从服务事件前缀(用于主从同步)
	NodePrefix     string    `binding:""` // 节点事件前缀
	ShardCount     int       `binding:""` // 分段锁数量
	NatsConf       *NatsConf `binding:""` // nats配置
}

type NatsConf struct {
	EndPoints            []string      `binding:""` // nats地址
	UserName             string        `binding:""` // nats用户名
	Password             string        `binding:""` // nats密码
	Token                string        `binding:""` // nats token
	Secure               string        `binding:""` // nats secure
	Cert                 string        `binding:""` // 证书
	CertKey              string        `binding:""` // 证书密钥
	CAs                  string        `binding:""` // ca证书
	MaxReconnects        int           `binding:""` // 最大重连次数
	ReconnectWait        time.Duration `binding:""` // 重连间隔
	Timeout              time.Duration `binding:""` // 连接超时时间
	PingInterval         time.Duration `binding:""` // ping间隔时间
	PingMaxOutstanding   int           `binding:""` // 最大未响应ping数
	ReconnectBufSize     int           `binding:""` // 重连缓冲区大小
	SubPendingMsgLimit   int           `binding:""` // 订阅 pending 最大消息数(用于提升高突发下的缓冲能力)
	SubPendingBytesLimit int           `binding:""` // 订阅 pending 最大字节数(用于提升高突发下的缓冲能力)
}

type ServiceLogConf struct {
	Enable bool            `binding:""` // 是否开启独立logger
	Config *log.LoggerConf // 日志配置
}

type WorkerStrategyConfig struct {
	Name           string                  `binding:""` // 策略名称
	Params         map[string]interface{}  `binding:""` // 策略参数,如果是复合策略,需要固定给一个map["mode"]="all/any"
	MinWorkerNum   int32                   `binding:""` // 最小工作线程数量(只有开启了动态worker扩展,这个值才会生效)
	MaxWorkerNum   int32                   `binding:""` // 最大工作线程数量(只有开启了动态worker扩展,这个值才会生效)
	GrowthFactor   float64                 `binding:""` // 扩容因子(线程池的数量=当前线程池数量*扩容因子)
	ShrinkFactor   float64                 `binding:""` // 缩容因子(线程池的数量=当前线程池数量*缩容因子)
	ResizeCoolDown time.Duration           `binding:""` // 缩容冷却时间(默认1秒)(当负载小于最小负载时,则关闭多余的线程)
	Subs           []*WorkerStrategyConfig `binding:""` // 子策略，复合策略才有
}

// MultiLevelQueueConf 多优先级队列配置
// 职责：配置多优先级队列的调度策略和各优先级的批量处理大小
// 仅在 QueueMode="priority" 时生效
type MultiLevelQueueConf struct {
	// Strategy 调度策略
	// - "absolute": 绝对优先策略（始终优先处理高优先级消息，可能导致低优先级饥饿）
	// - "weighted": 加权策略（按权重分配处理机会，兼顾各优先级）
	// - "fairness": 公平策略（确保所有优先级都有处理机会，防止饥饿）
	// 默认: "absolute"
	Strategy def.ScheduleStrategy `binding:""`

	// TotalBatchLimit 单次处理的总批次上限
	// 控制每次循环最多处理多少条消息，避免长时间阻塞
	// 默认: 32
	TotalBatchLimit int `binding:""`

	// PriorityBatches 各优先级的批量处理配置
	// key: 优先级（PrioritySys/Urgent/High/Normal/Low/Batch）
	// value: 该优先级的批量大小和权重配置
	// 示例: {"sys":{BatchSize:32, Weight:20}, "urgent":{BatchSize:16, Weight:10}}
	PriorityBatches map[def.Priority]*PriorityConfig `binding:""`
}

// MultiLevelWorkerConf 兼容旧配置（已废弃，请使用 MultiLevelQueueConf）
type MultiLevelWorkerConf struct {
	WaitMode        string                           `binding:""` // 等待模式: busy / cond (默认busy)
	Strategy        def.ScheduleStrategy             `binding:""` // 调度策略: absolute / weighted / fair (默认absolute)
	TotalBatchLimit int                              `binding:""` // 总批次上限(默认128)
	PriorityBatches map[def.Priority]*PriorityConfig `binding:""` // 各优先级批量大小 {"sys":32, "urgent":16, "high":12, "normal":8}
}

// PriorityConfig 单个优先级配置
// 职责：配置单个优先级队列的批量处理大小和调度权重
type PriorityConfig struct {
	// BatchSize 批量处理大小
	// 单次从该优先级队列最多弹出的消息数量
	// 建议: 高优先级设置较大值（如32），低优先级设置较小值（如4）
	BatchSize int `json:"batch_size"`

	// Weight 调度权重
	// 仅在 Strategy="weighted" 时生效
	// 权重越大，获得的处理机会越多
	// 建议: 高优先级设置较大权重（如20），低优先级设置较小权重（如1）
	Weight int `json:"weight"`
}

// WorkerIdlerConf 工作线程空闲控制配置
// 职责：配置Worker在无消息时的等待策略，平衡CPU占用和响应延迟
type WorkerIdlerConf struct {
	// EnableCond 是否启用条件变量等待
	// - true:  空闲时使用条件变量阻塞（节省CPU，但唤醒有开销）
	// - false: 空闲时使用指数退避睡眠（CPU占用略高，但响应更快）
	// 默认: false
	EnableCond bool `binding:""`

	// BackoffBaseDelay 退避基础延迟时间
	// 第一次空闲时的睡眠时间
	// 默认: 1微秒
	BackoffBaseDelay time.Duration `binding:""`

	// BackoffMaxDelay 退避最大延迟时间
	// 连续空闲时睡眠时间的上限
	// 默认: 16微秒
	BackoffMaxDelay time.Duration `binding:""`

	// BackoffMaxRetries 退避最大重试次数
	// 睡眠时间翻倍的最大次数
	// 默认: 3
	BackoffMaxRetries int `binding:""`

	// MaxIdleBeforeBackoff 开始退避前的最大空闲次数
	// 前N次空闲不睡眠，直接重试（适用于高频场景）
	// 默认: 1000
	MaxIdleBeforeBackoff int `binding:""`
}

type DeDuplicatorConf struct {
	DeDuplicatorType     string        `binding:""` // 去重处理器类型(ttl和lru)
	DeDuplicatorTTL      time.Duration `binding:""`
	DeDuplicatorCleanTTL time.Duration `binding:""`
	DeDuplicatorSize     int           `binding:""`
}

// WorkerSchedulePolicy 工作线程调度策略配置
// 职责：配置Worker的数量、扩缩容策略、空闲控制和多优先级队列
type WorkerSchedulePolicy struct {
	// InitialWorkerNum 初始工作线程数量
	// 启动时创建的Worker数量
	// 建议: 单核场景设为1，多核场景设为CPU核心数的1-2倍
	// 默认: 1
	InitialWorkerNum int32 `binding:""`

	// VirtualWorkerRate 虚拟节点倍率
	// 一致性哈希环中每个Worker对应的虚拟节点数量
	// 值越大，消息分布越均匀，但哈希计算开销越大
	// 建议: 10-24之间
	// 默认: 24
	VirtualWorkerRate int `binding:""`

	// EnableAutoScaling 是否启用自动扩缩容
	// true: 根据队列负载动态调整Worker数量
	// false: Worker数量固定为InitialWorkerNum
	// 默认: false
	EnableAutoScaling bool `binding:""`

	// ScalingStrategy 自动扩缩容策略配置
	// 仅在 EnableAutoScaling=true 时生效
	ScalingStrategy *WorkerStrategyConfig `binding:""`

	// IdlerConf 空闲控制配置
	// 配置Worker在无消息时的等待策略
	IdlerConf *WorkerIdlerConf `binding:""`

	// MultiLevelQueueConf 多优先级队列配置
	// 仅在 MailboxConf.QueueMode="priority" 时生效
	MultiLevelQueueConf *MultiLevelQueueConf `binding:""`

	// MultiLevelConf 兼容旧配置（已废弃，请使用 MultiLevelQueueConf）
	MultiLevelConf *MultiLevelWorkerConf `binding:""`
}

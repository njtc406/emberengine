package def

// MiddlewareAction 中间件动作，决定消息的后续处理方式
type MiddlewareAction int

const (
	// ActionContinue 继续执行后续中间件和消息处理
	ActionContinue MiddlewareAction = iota
	// ActionReject 拒绝消息入队，返回错误给调用方
	ActionReject
	// ActionSkip 跳过后续中间件，直接入队（用于快速路径）
	ActionSkip
)

type MailboxJobType int32

const (
	MailboxJobTypeNone               MailboxJobType = iota
	MailboxJobTypeRpc                               // rpc消息
	MailboxJobTypeEvent                             // 订阅事件(来自event_bus的消息)
	MailboxJobTypeTimer                             // 定时器
	MailboxJobTypeConcurrentCallback                // 并发回调
	MailboxJobTypeSysCtl                            // 系统控制类
)

// RWMode 读写模式标记，用于 Mailbox 级读写分离
type RWMode int32

const (
	// RWModeWrite 写操作（默认值）——独占执行，与其他任何操作互斥
	// 零值设计：未标记的 Job 默认为写操作，保证向后兼容和安全兜底
	RWModeWrite RWMode = iota
	// RWModeRead 读操作——可与其他 Read 操作并发执行，但与 Write 互斥
	RWModeRead
)

// ---- RW 合并 context 结构体（减少 WithValue 分配） ----

// RWContextInfo 合并 RWMode 和 SourceService 为单次 context.WithValue 注入
type RWContextInfo struct {
	Mode          RWMode
	SourceService string
}

type rwContextKeyType struct{}

// RWContextKey 用于在 context 中注入合并的 RW 信息（替代 RWModeContextKey + RWSourceServiceKey）
var RWContextKey = rwContextKeyType{}

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

// rwModeContextKeyType 是 RWModeContextKey 的类型，避免 context key 冲突
type rwModeContextKeyType struct{}

// RWModeContextKey 用于在 context 中注入 RWMode 信息
// ReadOnly handler 的 context 会携带此 key，值为 RWModeRead
// 业务层可通过 ctx.Value(def.RWModeContextKey) 检测当前是否在 ReadOnly 上下文中
var RWModeContextKey = rwModeContextKeyType{}

// rwSourceServiceKeyType 是 RWSourceServiceKey 的类型
type rwSourceServiceKeyType struct{}

// RWSourceServiceKey 框架内部使用，标记注入 RWModeRead 的源 Service 名称。
// 用于 Service.PostJob 中的自投递检测：仅当源 Service 与目标 Service 相同时才拦截，
// 避免误拦截跨 Service 的合法 RPC 调用。
// 业务层不应使用此 key。
var RWSourceServiceKey = rwSourceServiceKeyType{}

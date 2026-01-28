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

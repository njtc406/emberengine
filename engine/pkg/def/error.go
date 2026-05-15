package def

import (
	"errors"
)

// ============================================================================
// 系统错误定义
//
// 当前所有 sentinel 均使用 errors.New()，不含错误码。
// 后续新增 sentinel 推荐使用 errorx.New(code, msg) 以获得错误码、错误链和
// 结构化字段能力。
//
// 错误码分段规范（code 为 int，建议 4 位）：
//
//	分段       | 范围        | 模块
//	-----------|-------------|---------------------------
//	core       | 1000-1099   | core/service 生命周期
//	mailbox    | 1100-1199   | mailbox/worker/queue
//	rpc        | 1200-1299   | RPC 调用链 (call/send/bus)
//	config     | 1300-1399   | 配置加载/校验
//	cluster    | 1400-1499   | 集群/服务发现/选举
//	event      | 1500-1599   | 事件系统
//	node       | 1600-1699   | Node 生命周期
//	router     | 1700-1799   | 路由
//	sysmodule  | 1800-1899   | 内置系统模块 (gate/http/ws/db)
//	general    | 9000-9099   | 通用/序列化/token/http
//
// 命名规则：
//   - sentinel 变量名前缀为 Err，如 ErrRPCCallTimeout
//   - msg 使用英文小写短语，不带标点
//   - 新增 sentinel 必须在上方注册分段，避免码冲突
//
// 迁移说明：
//   - 现有 errors.New() sentinel 不强制替换，只在需要跨节点识别错误码时迁移
//   - 新增 sentinel 优先使用 errorx.New(code, msg)
// ============================================================================

var (
	ErrModuleNotInitialized        = errors.New("module not initialized")                                  // 模块未初始化
	ErrModuleHadRegistered         = errors.New("module had registered")                                   // 已经注册过该模块
	ErrMailboxWorkerChannelNotInit = errors.New("mailbox worker user channel not init")                    // 邮箱工作通道未初始化
	ErrMailboxWorkerClosed         = errors.New("mailbox worker closed")                                   // 邮箱工作线程已关闭
	ErrEventChannelIsFull          = errors.New("event channel is full")                                   // 事件通道已满
	ErrMailboxNotRunning           = errors.New("mailbox not running")                                     // 邮箱未运行
	ErrMailboxWorkerIsFull         = errors.New("mailbox worker is full")                                  // 邮箱工作线程已满
	ErrMailboxWorkerNotFound       = errors.New("mailbox worker not found")                                // 邮箱工作线程未找到
	ErrWorkerClosed                = errors.New("worker is closed")                                        // Worker已关闭
	ErrRPCCallTimeout              = errors.New("rpc call timeout")                                        // RPC 调用超时
	ErrServiceNotFound             = errors.New("service not found")                                       // 服务未找到
	ErrServiceIsRunning            = errors.New("service is running")                                      // 服务正在运行
	ErrServiceIsClosedOrExited     = errors.New("service is closed or exited")                             // 服务已关闭或已退出
	ErrRPCCallFailed               = errors.New("rpc call failed")                                         // RPC 调用失败
	ErrParamNotMatch               = errors.New("param not match")                                         // 参数不匹配
	ErrInputParamCantUseStruct     = errors.New("input param can't use struct, must be ptr")               // 输入参数不能使用结构体,必须是结构体指针
	ErrInputParamNotMatch          = errors.New("input param not match")                                   // 输入参数不匹配
	ErrOutputParamNotMatch         = errors.New("output param not match")                                  // 输出参数不匹配
	ErrMethodNotFound              = errors.New("method not found")                                        // 方法未找到
	ErrRPCHadClosed                = errors.New("rpc had closed")                                          // RPC 已经关闭
	ErrMsgSerializeFailed          = errors.New("message serialize failed")                                // 消息序列化失败
	ErrTokenExpired                = errors.New("token expired")                                           // token 过期
	ErrTokenInvalid                = errors.New("token invalid")                                           // token 无效
	ErrJsonMarshalFailed           = errors.New("json marshal failed")                                     // json 序列化失败
	ErrJsonUnmarshalFailed         = errors.New("json unmarshal failed")                                   // json 反序列化失败
	ErrHttpCreateRequestFailed     = errors.New("http create request failed")                              // http 创建请求失败
	ErrHttpRequestFailed           = errors.New("http request failed")                                     // http 请求失败
	ErrHttpReadResponseFailed      = errors.New("http read response failed")                               // http 读取响应失败
	ErrServiceIsUnavailable        = errors.New("service is unavailable")                                  // 服务不可用
	ErrDiscoveryConfNotFound       = errors.New("discovery conf not found")                                // 配置中心未找到
	ErrETCDNotInit                 = errors.New("etcd not init")                                           // etcd 未初始化
	ErrHandleMessagePanic          = errors.New("handle message panic")                                    // 处理消息时发生 panic
	ErrCallbacksIsEmpty            = errors.New("callbacks is empty")                                      // 回调函数为空
	ErrCantFoundRedisClient        = errors.New("cant found redis client")                                 // 未找到 redis 客户端
	ErrMysqlNotInit                = errors.New("mysql not init")                                          // mysql 未初始化
	ErrPrimarySecondNotSupported   = errors.New("primary second not supported")                            // 不支持主从
	ErrSelectEmptyResult           = errors.New("select empty result")                                     // 查询结果为空
	ErrEnvelopeNotFound            = errors.New("envelope not found")                                      // 找不到 envelope
	RouterNotFound                 = errors.New("router not found")                                        // 未找到路由
	ErrRepeatExecute               = errors.New("repeat execute")                                          // 重复执行
	ErrTimerReuse                  = errors.New("timer reuse")                                             // 定时器复用
	ErrMailboxSuspended            = errors.New("mailbox suspended")                                       // 邮箱已挂起
	ErrMailboxMiddlewareRejected   = errors.New("mailbox middleware rejected")                             // 中间件拒绝消息
	ErrEventIsUnRef                = errors.New("event is unref")                                          // 事件已经被释放
	ErrRpcMsgMetaOrDataIsNil       = errors.New("rpc msg meta or data is nil")                             // rpc 消息元数据或数据为空
	ErrJobHandlerNotFound          = errors.New("job handler not found")                                   // 任务处理函数未找到
	ErrJobTimeout                  = errors.New("job timeout")                                             // 任务超时
	ErrEventHandlerNotFound        = errors.New("event handler not found")                                 // 事件处理函数未找到
	ErrReadOnlyPostJob             = errors.New("ReadOnly handler cannot PostJob (self-posting detected)") // ReadOnly handler 自投递拒绝
)

//type RpcErr string
//
//func (re RpcErr) Error() string {
//	return fmt.Sprintf("rpc error: %s", string(re))
//}

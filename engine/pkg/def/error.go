package def

import (
	"errors"
)

// 定义系统错误

var (
	ErrModuleNotInitialized      = errors.New("module not initialized")                    // 模块未初始化
	ErrModuleHadRegistered       = errors.New("module had registered")                     // 已经注册过该模块
	ErrEventChannelIsFull        = errors.New("111111111event channel is full")            // 事件通道已满
	ErrSysEventChannelIsFull     = errors.New("sys event channel is full")                 // 系统事件通道已满
	ErrMailboxNotRunning         = errors.New("mailbox not running")                       // 邮箱未运行
	ErrMailboxWorkerIsFull       = errors.New("mailbox worker is full")                    // 邮箱工作线程已满
	ErrRPCCallTimeout            = errors.New("rpc call timeout")                          // RPC 调用超时
	ErrServiceNotFound           = errors.New("service not found")                         // 服务未找到
	ErrServiceIsRunning          = errors.New("service is running")                        // 服务正在运行
	ErrServiceIsClosedOrExited   = errors.New("service is closed or exited")               // 服务已关闭或已退出
	ErrRPCCallFailed             = errors.New("rpc call failed")                           // RPC 调用失败
	ErrParamNotMatch             = errors.New("param not match")                           // 参数不匹配
	ErrInputParamCantUseStruct   = errors.New("input param can't use struct, must be ptr") // 输入参数不能使用结构体,必须是结构体指针
	ErrInputParamNotMatch        = errors.New("input param not match")                     // 输入参数不匹配
	ErrOutputParamNotMatch       = errors.New("output param not match")                    // 输出参数不匹配
	ErrMethodNotFound            = errors.New("method not found")                          // 方法未找到
	ErrRPCHadClosed              = errors.New("rpc had closed")                            // RPC 已经关闭
	ErrMsgSerializeFailed        = errors.New("message serialize failed")                  // 消息序列化失败
	ErrTokenExpired              = errors.New("token expired")                             // token 过期
	ErrTokenInvalid              = errors.New("token invalid")                             // token 无效
	ErrJsonMarshalFailed         = errors.New("json marshal failed")                       // json 序列化失败
	ErrJsonUnmarshalFailed       = errors.New("json unmarshal failed")                     // json 反序列化失败
	ErrHttpCreateRequestFailed   = errors.New("http create request failed")                // http 创建请求失败
	ErrHttpRequestFailed         = errors.New("http request failed")                       // http 请求失败
	ErrHttpReadResponseFailed    = errors.New("http read response failed")                 // http 读取响应失败
	ErrServiceIsUnavailable      = errors.New("service is unavailable")                    // 服务不可用
	ErrDiscoveryConfNotFound     = errors.New("discovery conf not found")                  // 配置中心未找到
	ErrETCDNotInit               = errors.New("etcd not init")                             // etcd 未初始化
	ErrHandleMessagePanic        = errors.New("handle message panic")                      // 处理消息时发生 panic
	ErrCallbacksIsEmpty          = errors.New("callbacks is empty")                        // 回调函数为空
	ErrCantFoundRedisClient      = errors.New("cant found redis client")                   // 未找到 redis 客户端
	ErrMysqlNotInit              = errors.New("mysql not init")                            // mysql 未初始化
	ErrPrimarySecondNotSupported = errors.New("primary second not supported")              // 不支持主从
	ErrSelectEmptyResult         = errors.New("select empty result")                       // 查询结果为空
	ErrEnvelopeNotFound          = errors.New("envelope not found")                        // 找不到 envelope
)

//type RpcErr string
//
//func (re RpcErr) Error() string {
//	return fmt.Sprintf("rpc error: %s", string(re))
//}

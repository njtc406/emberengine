package errdef

import (
	"errors"
)

// 定义系统错误

var (
	ModuleNotInitialized    = errors.New("module not initialized")                    // 模块未初始化
	ModuleHadRegistered     = errors.New("module had registered")                     // 已经注册过该模块
	EventChannelIsFull      = errors.New("event channel is full")                     // 事件通道已满
	RPCCallTimeout          = errors.New("rpc call timeout")                          // RPC 调用超时
	ServiceNotFound         = errors.New("service not found")                         // 服务未找到
	ServiceIsRunning        = errors.New("service is running")                        // 服务正在运行
	RPCCallFailed           = errors.New("rpc call failed")                           // RPC 调用失败
	ParamNotMatch           = errors.New("param not match")                           // 参数不匹配
	InputParamCantUseStruct = errors.New("input param can't use struct, must be ptr") // 输入参数不能使用结构体,必须是结构体指针
	InputParamNotMatch      = errors.New("input param not match")                     // 输入参数不匹配
	OutputParamNotMatch     = errors.New("output param not match")                    // 输出参数不匹配
	MethodNotFound          = errors.New("method not found")                          // 方法未找到
	RPCHadClosed            = errors.New("rpc had closed")                            // RPC 已经关闭
	MsgSerializeFailed      = errors.New("message serialize failed")                  // 消息序列化失败
	TokenExpired            = errors.New("token expired")                             // token 过期
	TokenInvalid            = errors.New("token invalid")                             // token 无效
	JsonMarshalFailed       = errors.New("json marshal failed")                       // json 序列化失败
	JsonUnmarshalFailed     = errors.New("json unmarshal failed")                     // json 反序列化失败
	HttpCreateRequestFailed = errors.New("http create request failed")                // http 创建请求失败
	HttpRequestFailed       = errors.New("http request failed")                       // http 请求失败
	HttpReadResponseFailed  = errors.New("http read response failed")                 // http 读取响应失败
	ServiceIsUnavailable    = errors.New("service is unavailable")                    // 服务不可用
	DiscoveryConfNotFound   = errors.New("discovery conf not found")                  // 配置中心未找到
	ETCDNotInit             = errors.New("etcd not init")                             // etcd 未初始化
	HandleMessagePanic      = errors.New("handle message panic")                      // 处理消息时发生 panic
	CallbacksIsEmpty        = errors.New("callbacks is empty")                        // 回调函数为空
)

//type RpcErr string
//
//func (re RpcErr) Error() string {
//	return fmt.Sprintf("rpc error: %s", string(re))
//}

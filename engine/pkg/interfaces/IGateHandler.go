// Package interfaces
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/8/14 0014 0:07
// 最后更新:  yr  2025/8/14 0014 0:07
package interfaces

import "context"

type IConn interface {
	GetConnId() string
	GetClientIp() string
	Send(msg []byte) error
	Close() error
}

type IAdapterHandler interface {
	OnConnect(c IConn)
	OnMessage(c IConn, msg []byte)
	OnClose(c IConn)
}

type IProtocolAdapter interface {
	// 启动监听
	ListenAndServe(svc IService, config interface{}) error
	// 关闭
	Shutdown(ctx context.Context) error
	// 绑定业务消息处理器
	SetHandler(h IAdapterHandler)
}

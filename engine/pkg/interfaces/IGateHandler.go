// Package interfaces
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/8/14 0014 0:07
// 最后更新:  yr  2025/8/14 0014 0:07
package interfaces

import "context"

type IConn interface {
	GetClientIp() string
	ReadMessage() ([]byte, error)
	Send(msg []byte) error
	Close() error
}

type IAdapterHandler interface {
	OnConnect(s ISession) error
	OnDisconnect(s ISession, reason string)
	OnMessage(s ISession, msg []byte)
	OnClose(s ISession)
	GetProcessor() IMessageProcessor
}

type IProtocolAdapter interface {
	// 启动监听
	ListenAndServe(md IModule, config interface{}) error
	// 关闭
	Shutdown(ctx context.Context) error

	SetSessionMgr(sessionMgr ISessionManager)
	GetSessionMgr() ISessionManager
}

type ISessionManager interface {
	Bind(uid int64, conn IConn)           // 绑定新连接
	Kick(sessionID uint64, reason string) // 断开连接
	KickByUid(uid int64)                  // 根据uid断开
	KickAll()                             // 断开所有连接
	Broadcast(msg []byte)                 // 广播
	GetSession(sessionID uint64) ISession // 查询会话
	GetSessionByUid(uid int64) ISession   // 查询会话
	SetHandler(handler IAdapterHandler)
	GetHandler() IAdapterHandler
}

type ISession interface {
	GetUid() int64
	GetSessionId() uint64
	GetConn() IConn
	Close()
	IsClosed() bool
	Send(data []byte)
	StartSender()
}

type IMessageProcessor interface {
	// Marshal 将消息编码为二进制，返回 byte slice
	Marshal(msgId int32, msg []byte) ([]byte, error)
	// Unmarshal 将二进制解析为消息对象
	Unmarshal(data []byte) (IMessagePack, error)
}

type IMessagePack interface {
	GetMsgId() int32
	GetRawMsg() []byte
}

package processor

type IProcessor interface {
	// must goroutine safe
	MsgRoute(sessionId int64, clientId string, msg interface{}) error
	//must goroutine safe
	UnknownMsgRoute(sessionId int64, clientId string, msg interface{})
	// connect event
	ConnectedRoute(sessionId int64, clientId string)
	DisConnectedRoute(sessionId int64, clientId string)

	// must goroutine safe
	Unmarshal(data []byte) (interface{}, error)
	// must goroutine safe
	Marshal(id int32, msg interface{}) ([]byte, error)
}

type IRawProcessor interface {
	IProcessor

	SetByteOrder(littleEndian bool)
	SetRawMsgHandler(handle RawMessageHandler)
	MakeRawMsg(msgId int32, msg []byte, pbRawPackInfo *PBRawPackInfo)
	SetUnknownMsgHandler(unknownMessageHandler UnknownRawMessageHandler)
	SetConnectedHandler(connectHandler RawConnectHandler)
	SetDisConnectedHandler(disconnectHandler RawConnectHandler)
}

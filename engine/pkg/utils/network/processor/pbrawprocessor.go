package processor

import (
	"encoding/binary"
	"errors"
	"reflect"
	"sync"
)

type RawMessageInfo struct {
	msgType    reflect.Type
	msgHandler RawMessageHandler
}

type RawMessageHandler func(sessionId int64, clientId string, msgId int32, msg []byte)
type RawConnectHandler func(sessionId int64, clientId string)
type UnknownRawMessageHandler func(sessionId int64, clientId string, msg []byte)

const HeaderSize = 4

type PBRawProcessor struct {
	msgHandler            RawMessageHandler
	LittleEndian          bool
	unknownMessageHandler UnknownRawMessageHandler
	connectHandler        RawConnectHandler
	disconnectHandler     RawConnectHandler
}

type PBRawPackInfo struct {
	id     int32
	rawMsg []byte
}

func (slf *PBRawPackInfo) Reset() {
	slf.id = 0
	slf.rawMsg = slf.rawMsg[:0]
}

func (slf *PBRawPackInfo) GetMsgId() int32 {
	return slf.id
}

func (slf *PBRawPackInfo) GetMsg() []byte {
	return slf.rawMsg
}

func (slf *PBRawPackInfo) SetPackInfo(id int32, rawMsg []byte) {
	slf.id = id
	slf.rawMsg = rawMsg
}

var pbPackPool = sync.Pool{
	New: func() interface{} {
		return &PBRawPackInfo{}
	},
}

func NewPBRawProcessor() *PBRawProcessor {
	processor := &PBRawProcessor{}
	return processor
}

func (pbRawProcessor *PBRawProcessor) SetByteOrder(littleEndian bool) {
	pbRawProcessor.LittleEndian = littleEndian
}

// must goroutine safe
func (pbRawProcessor *PBRawProcessor) MsgRoute(sessionId int64, clientId string, msg interface{}) error {
	pPackInfo := msg.(*PBRawPackInfo)
	defer pbPackPool.Put(pPackInfo)
	pbRawProcessor.msgHandler(sessionId, clientId, pPackInfo.id, pPackInfo.rawMsg)
	return nil
}

// must goroutine safe
func (pbRawProcessor *PBRawProcessor) Unmarshal(data []byte) (interface{}, error) {
	if len(data) < HeaderSize {
		return nil, nil
	}
	var msgId int32
	if pbRawProcessor.LittleEndian == true {
		msgId = int32(binary.LittleEndian.Uint32(data[:HeaderSize]))
	} else {
		msgId = int32(binary.BigEndian.Uint32(data[:HeaderSize]))
	}

	// TODO 之后可能会在这里加上压缩,序列化类型等等

	pack := pbPackPool.New().(*PBRawPackInfo)
	pack.SetPackInfo(msgId, data[HeaderSize:])

	return pack, nil
}

// must goroutine safe
func (pbRawProcessor *PBRawProcessor) Marshal(id int32, msg interface{}) ([]byte, error) {
	rawMsg, ok := msg.([]byte)
	if !ok {
		return nil, errors.New("invalid proto message type")
	}

	// **一次性分配完整 buffer，避免 append 额外的内存分配**
	buff := make([]byte, HeaderSize+len(rawMsg))

	// 写入消息 ID
	if pbRawProcessor.LittleEndian {
		binary.LittleEndian.PutUint32(buff[:HeaderSize], uint32(id))
	} else {
		binary.BigEndian.PutUint32(buff[:HeaderSize], uint32(id))
	}

	// **直接拷贝 PB 数据，避免 append**
	copy(buff[HeaderSize:], rawMsg)
	return buff, nil
}

func (pbRawProcessor *PBRawProcessor) SetRawMsgHandler(handle RawMessageHandler) {
	pbRawProcessor.msgHandler = handle
}

func (pbRawProcessor *PBRawProcessor) MakeRawMsg(msgId int32, msg []byte, pbRawPackInfo *PBRawPackInfo) {
	pbRawPackInfo.id = msgId
	pbRawPackInfo.rawMsg = msg
}

func (pbRawProcessor *PBRawProcessor) UnknownMsgRoute(sessionId int64, clientId string, msg interface{}) {
	defer func() {
		if msg != nil {
			pbPackPool.Put(msg)
		}
	}()
	if pbRawProcessor.unknownMessageHandler == nil {
		return
	}
	pbRawProcessor.unknownMessageHandler(sessionId, clientId, msg.([]byte))
}

// connect event
func (pbRawProcessor *PBRawProcessor) ConnectedRoute(sessionId int64, clientId string) {
	if pbRawProcessor.connectHandler == nil {
		return
	}
	pbRawProcessor.connectHandler(sessionId, clientId)
}

func (pbRawProcessor *PBRawProcessor) DisConnectedRoute(sessionId int64, clientId string) {
	if pbRawProcessor.disconnectHandler == nil {
		return
	}
	pbRawProcessor.disconnectHandler(sessionId, clientId)
}

func (pbRawProcessor *PBRawProcessor) SetUnknownMsgHandler(unknownMessageHandler UnknownRawMessageHandler) {
	pbRawProcessor.unknownMessageHandler = unknownMessageHandler
}

func (pbRawProcessor *PBRawProcessor) SetConnectedHandler(connectHandler RawConnectHandler) {
	pbRawProcessor.connectHandler = connectHandler
}

func (pbRawProcessor *PBRawProcessor) SetDisConnectedHandler(disconnectHandler RawConnectHandler) {
	pbRawProcessor.disconnectHandler = disconnectHandler
}

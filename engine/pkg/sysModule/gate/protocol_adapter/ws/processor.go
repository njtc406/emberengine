// Package ws
// 模块名: 消息处理器
// 功能描述: 描述
// 作者:  yr  2025/8/17 0017 1:29
// 最后更新:  yr  2025/8/17 0017 1:29
package ws

import (
	"encoding/binary"
	"fmt"

	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
)

const HeaderSize = 4

type Processor struct {
	LittleEndian    bool
	CopyOnUnmarshal bool // 是否强制拷贝
}

func NewProcessor(littleEndian, copyOnUnmarshal bool) *Processor {
	return &Processor{
		LittleEndian:    littleEndian,
		CopyOnUnmarshal: copyOnUnmarshal,
	}
}
func (p *Processor) Marshal(msgId int32, msg []byte) ([]byte, error) {
	size := HeaderSize + len(msg)
	buff := make([]byte, size)

	if p.LittleEndian {
		binary.LittleEndian.PutUint32(buff[:HeaderSize], uint32(msgId))
	} else {
		binary.BigEndian.PutUint32(buff[:HeaderSize], uint32(msgId))
	}
	copy(buff[HeaderSize:], msg)
	return buff, nil
}

func (p *Processor) Unmarshal(data []byte) (inf.IMessagePack, error) {
	if len(data) < HeaderSize {
		return nil, fmt.Errorf("data too short")
	}

	var msgId int32
	if p.LittleEndian {
		msgId = int32(binary.LittleEndian.Uint32(data[:HeaderSize]))
	} else {
		msgId = int32(binary.BigEndian.Uint32(data[:HeaderSize]))
	}

	raw := data[HeaderSize:]
	if p.CopyOnUnmarshal {
		copied := make([]byte, len(raw))
		copy(copied, raw)
		raw = copied
	}

	pack := pbPackPool.Get()
	pack.SetPackInfo(msgId, raw)
	return pack, nil
}

type PBRawPackInfo struct {
	id     int32
	rawMsg []byte
}

func (p *PBRawPackInfo) reset() {
	p.id = 0
	p.rawMsg = nil
}

func (p *PBRawPackInfo) SetPackInfo(id int32, rawMsg []byte) {
	p.id = id
	p.rawMsg = rawMsg
}

func (p *PBRawPackInfo) GetMsgId() int32 {
	return p.id
}

func (p *PBRawPackInfo) GetRawMsg() []byte {
	return p.rawMsg
}

var pbPackPool = pool.NewSyncPoolWrapper(
	func() *PBRawPackInfo {
		return &PBRawPackInfo{}
	},
	pool.NewStatsRecorder("PBRawPackInfo"),
	pool.WithReset(func(p *PBRawPackInfo) {
		p.reset()
	}),
)

//var buffPool = pool.NewSyncPoolWrapper(
//	func() *[]byte {
//		buff := make([]byte, 2048*1024) // 消息最大2M
//		return &buff
//	},
//	pool.NewStatsRecorder("msgBuffer"),
//	pool.WithReset(func(b *[]byte) {
//		*b = (*b)[:0]
//	}),
//)

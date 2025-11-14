// Package nt
// @Title  title
// @Description  desc
// @Author  yr  2025/4/15
// @Update  yr  2025/4/15
package nt

import (
	"github.com/nats-io/nats.go"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/rpc/message/msgenvelope"
	"github.com/njtc406/emberengine/engine/pkg/rpc/remote/handler"
	"google.golang.org/protobuf/proto"
)

type NatsListener struct {
	cliFactory inf.IRpcSenderFactory
}

func (n *NatsListener) Handle(msg *nats.Msg) {
	req := msgenvelope.NewMessage()
	defer msgenvelope.ReleaseMessage(req)
	err := proto.Unmarshal(msg.Data, req)
	if err != nil {
		log.SysLogger.Errorf("unmarshal nats message error: %v", err)
		return
	}

	if err = handler.RpcMessageHandler(n.cliFactory, req); err != nil {
		log.SysLogger.Errorf("handle nats message error: %v  req:%+v", err, req)
	}
}

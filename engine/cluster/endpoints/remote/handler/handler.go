// Package handler
// @Title  title
// @Description  desc
// @Author  yr  2024/12/18
// @Update  yr  2024/12/18
package handler

import (
	"github.com/njtc406/emberengine/engine/actor"
	"github.com/njtc406/emberengine/engine/inf"
	"github.com/njtc406/emberengine/engine/monitor"
	"github.com/njtc406/emberengine/engine/msgenvelope"
	"github.com/njtc406/emberengine/engine/utils/log"
	"github.com/njtc406/emberengine/engine/utils/serializer"
)

// TODO 处理ReqId重复发送的问题

func RpcMessageHandler(sf inf.IRpcSenderFactory, req *actor.Message) error {
	if req.Reply {
		// 回复
		// 需要回复的信息都会加入monitor中,找到对应的信封数据
		if envelope := monitor.GetRpcMonitor().Remove(req.ReqId); envelope != nil {
			// 异步回调,直接发送到对应服务处理,服务处理完后会自己释放envelope
			sender := envelope.GetSender()
			if sender != nil && sender.IsClosed() {
				// 调用者已经下线,丢弃回复
				msgenvelope.ReleaseMsgEnvelope(envelope)
				return nil
			}
			// 解析回复数据
			response, err := serializer.Deserialize(req.Response, req.TypeName, req.TypeId)
			if err != nil {
				msgenvelope.ReleaseMsgEnvelope(envelope)
				return err
			}
			envelope.SetReply()
			envelope.SetRequest(nil)
			envelope.SetNeedResponse(false) // 已经是回复了
			envelope.SetResponse(response)
			envelope.SetErrStr(req.Err)

			//log.SysLogger.Debugf("call back envelope: %+v", envelope)

			if envelope.NeedCallback() {
				return sender.PostUserMessage(envelope)
			} else {
				// 同步回调,回复结果
				envelope.Done()
				return nil
			}
		} else {
			// 已经超时,丢弃返回
			log.SysLogger.Warnf("rpc call timeout, envelope not found: %s", req.String())
			return nil
		}
	} else {
		// 调用
		request, err := serializer.Deserialize(req.Request, req.TypeName, req.TypeId)
		if err != nil {
			return err
		}

		// 构建消息
		envelope := msgenvelope.NewMsgEnvelope()
		envelope.SetHeaders(req.MessageHeader)
		envelope.SetMethod(req.Method)
		envelope.SetReceiverPid(req.ReceiverPid)
		if req.NeedResp {
			// 需要回复的才设置sender
			envelope.SetSenderPid(req.SenderPid)
			envelope.SetSender(sf.GetSender(req.SenderPid))
		}
		envelope.SetRequest(request)
		envelope.SetResponse(nil)
		envelope.SetReqId(req.ReqId)
		envelope.SetNeedResponse(req.NeedResp)

		return sf.GetSender(req.ReceiverPid).SendRequest(envelope)
	}
}

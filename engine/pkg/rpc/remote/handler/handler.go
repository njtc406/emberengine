// Package handler
// @Title  title
// @Description  desc
// @Author  yr  2024/12/18
// @Update  yr  2024/12/18
package handler

import (
	"github.com/njtc406/emberengine/engine/internal/message/msgenvelope"
	"github.com/njtc406/emberengine/engine/internal/monitor"
	"github.com/njtc406/emberengine/engine/pkg/actor"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/dedup"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/serializer"
)

func RpcMessageHandler(sf inf.IRpcSenderFactory, req *actor.Message) error {
	if req.Reply {
		// 回复
		// 需要回复的信息都会加入monitor中,找到对应的信封数据
		if envelope := monitor.GetRpcMonitor().Remove(req.ReqId); envelope != nil {
			meta := envelope.GetMeta()
			data := envelope.GetData()

			// 异步回调,直接发送到对应服务处理,服务处理完后会自己释放envelope
			sender := meta.GetDispatcher()
			if sender != nil && sender.IsClosed() {
				// 调用者已经下线,丢弃回复
				// TODO 这里需要想想能不能直接丢弃,看要不要带一个标记,比如true时表示这个回复必须处理,那么可能就需要重新加载service
				envelope.Release()
				return nil
			}
			// 解析回复数据
			response, err := serializer.Deserialize(req.Response, req.TypeName, req.TypeId)
			if err != nil {
				envelope.Release()
				return err
			}

			// TODO 这里需要注意,当相同的data被重复使用时,response可能被下一个覆盖,虽然按理说如果是call那么一定是排队的,但是怕以后忘记了,先注释一下
			// 后续如果有了其他的需求,再来考虑这里的覆盖问题
			data.SetReply()
			data.SetRequest(nil)
			data.SetNeedResponse(false) // 已经是回复了
			data.SetResponse(response)
			data.SetErrStr(req.Err)

			//log.SysLogger.Debugf("call back envelope: %+v", envelope)

			if meta.NeedCallback() {
				return sender.PostMessage(envelope)
			} else {
				// 同步回调,回复结果
				envelope.Done()
				return nil
			}
		} else {
			// 已经超时,丢弃返回
			fields := make(map[string]interface{})
			if req.MessageHeader != nil {
				for k, v := range req.MessageHeader {
					fields[k] = v
				}
			}

			log.SysLogger.WithFields(fields).Warnf("rpc call timeout, envelope not found: %s", req.String())
			return nil
		}
	} else {
		// 检查重复
		if dedup.GetRpcReqDuplicator().Seen(req.ReqId) {
			fields := make(map[string]interface{})
			if req.MessageHeader != nil {
				for k, v := range req.MessageHeader {
					fields[k] = v
				}
			}
			log.SysLogger.WithFields(fields).Errorf("duplicate reqId:%d rpc request: %s", req.ReqId, req.String())
			return nil
		}

		// 调用
		request, err := serializer.Deserialize(req.Request, req.TypeName, req.TypeId)
		if err != nil {
			return err
		}

		// 构建消息
		envelope := msgenvelope.NewMsgEnvelope()
		if req.Method == "RpcTestWithError" {
			log.SysLogger.Debugf("===============================%s", req.String())
		}
		envelope.SetHeaders(req.MessageHeader)
		if req.Method == "RpcTestWithError" {
			log.SysLogger.Debugf("===============================%+v", envelope.GetHeaders())
		}

		data := msgenvelope.NewData()
		data.SetMethod(req.Method)
		data.SetRequest(request)
		data.SetResponse(nil)

		data.SetNeedResponse(req.NeedResp)

		meta := msgenvelope.NewMeta()
		meta.SetReceiverPid(req.ReceiverPid)
		meta.SetReqId(req.ReqId)
		if req.NeedResp {
			// 需要回复的才设置sender
			meta.SetSenderPid(req.SenderPid)
			meta.SetDispatcher(sf.GetDispatcher(req.SenderPid))
		}
		envelope.SetMeta(meta)
		envelope.SetData(data)

		err = sf.GetDispatcher(req.ReceiverPid).SendRequest(envelope)
		if err != nil {
			return err
		}

		dedup.GetRpcReqDuplicator().MarkDone(req.ReqId)

		return nil
	}
}

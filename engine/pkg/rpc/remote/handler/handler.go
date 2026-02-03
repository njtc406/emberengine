// Package handler
// @Title  title
// @Description  desc
// @Author  yr  2024/12/18
// @Update  yr  2024/12/18
package handler

import (
	"context"
	"errors"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/monitor"
	"github.com/njtc406/emberengine/engine/pkg/rpc/message/msgenvelope"
	"github.com/njtc406/emberengine/engine/pkg/utils/codec"
	"github.com/njtc406/emberengine/engine/pkg/utils/dedup"
	"github.com/njtc406/emberengine/engine/pkg/utils/xcontext"
)

func RpcMessageHandler(sf inf.IRpcSenderFactory, req *actor.Message) error {
	headers := make(map[string]any, len(req.ContextHeaders)+2)
	for k, v := range req.ContextHeaders {
		headers[k] = v
	}

	if req.Reply {
		// 回复
		// 需要回复的信息都会加入monitor中,找到对应的信封数据
		if state := monitor.GetRpcMonitor().Remove(req.ReqId); state != nil {
			response, err := codec.DecodeFromAny(req.Response)
			if err == nil && req.Err != "" {
				err = errors.New(req.Err)
			}

			state.SetResult(response, err)
			state.Complete()
			return nil
		} else {
			// 已经超时,丢弃返回
			// 迟到的回复：通常是调用方已超时/取消后的正常现象，避免刷屏按 debug 处理。
			log.SysLogger.Debugf("rpc call late reply dropped (state not found): %s", req.String())
			return nil
		}
	} else {
		// 去重只对“有ReqId”的请求有意义（通常是 NeedResp=true 的 call/asyncCall）。
		// fire-and-forget 的 send 使用 ReqId=0，跳过去重以减少热路径开销。
		if req.ReqId != 0 {
			senderServiceUid := req.GetSenderPid().GetServiceUid()
			// TODO 需要考虑GetRpcReqDuplicator这里在不同的节点中使用不同的模式,TTL或者LRU,防止在高并发节点在TTL模式下被瞬间击穿,会导致map容量爆炸式增加
			if dedup.GetDeDuplicator().Seen(senderServiceUid, req.ReqId) {
				log.SysLogger.Errorf("duplicate reqId:%d rpc request: %s", req.ReqId, req.String())
				return nil
			}
		}

		// 调用：Request 为空时无需解码 Any（nil payload 的 send 是常见场景）
		var request interface{}
		if req.Request != nil {
			var err error
			request, err = codec.DecodeFromAny(req.Request)
			//request, err := serializer.Deserialize(req.Request, req.TypeName, req.TypeId)
			if err != nil {
				return err
			}
		}

		// 从 headers 构建 context
		ctx := xcontext.New(context.Background())
		ctx.AddHeaders(headers)

		// 构建消息
		envelope := msgenvelope.NewMsgEnvelope()
		envelope.SetDispatchKey(req.DispatcherKey)
		envelope.SetPriority(def.Priority(req.Priority))

		data := msgenvelope.NewData()
		data.SetMethod(req.Method)
		data.SetRequest(request)
		data.SetResponse(nil)

		data.SetNeedResponse(req.NeedResp)

		meta := msgenvelope.NewMeta()
		meta.SetReceiverPid(req.ReceiverPid)
		meta.SetReqId(req.ReqId)
		meta.SetDeadline(req.Deadline)

		if req.NeedResp {
			// 需要回复的才设置sender
			meta.SetSenderPid(req.SenderPid)
			meta.SetDispatcher(sf.GetDispatcher(req.SenderPid))
		}
		envelope.SetMeta(meta)
		envelope.SetData(data)

		err := sf.GetDispatcher(req.ReceiverPid).Deliver(ctx, envelope)
		if err != nil {
			envelope.Release()
			return err
		}

		return nil
	}
}

// Package handler
// @Title  title
// @Description  desc
// @Author  yr  2024/12/18
// @Update  yr  2024/12/18
package handler

import (
	"context"
	"errors"
	"fmt"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/authz"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/monitor"
	"github.com/njtc406/emberengine/engine/pkg/rpc/message/msgenvelope"
	"github.com/njtc406/emberengine/engine/pkg/utils/codec"
	"github.com/njtc406/emberengine/engine/pkg/utils/errorx"
	"github.com/njtc406/emberengine/engine/pkg/utils/xcontext"
)

type Handler struct {
	rpcMonitor *monitor.RpcMonitor
	logger     log.ILoggerX
	dedup      inf.IDeDuplicator
	authorizer *authz.Authorizer
}

func NewHandler(rm *monitor.RpcMonitor, logger log.ILoggerX, dedup inf.IDeDuplicator) *Handler {
	return &Handler{rpcMonitor: rm, logger: logger, dedup: dedup}
}

// SetAuthorizer 注入授权引擎（可选，nil 或未启用时跳过授权检查）。
func (h *Handler) SetAuthorizer(a *authz.Authorizer) {
	h.authorizer = a
}

// authorizeRequest 在 dispatch 前校验调用方是否有权访问目标服务的方法。
// 返回 nil 表示允许，非 nil 表示拒绝。
func (h *Handler) authorizeRequest(req *actor.Message) error {
	if h.authorizer == nil || !h.authorizer.IsEnabled() {
		return nil
	}
	sender := req.GetSenderPid()
	receiver := req.GetReceiverPid()
	if sender == nil || receiver == nil {
		return fmt.Errorf("authz: missing sender or receiver pid")
	}
	principal := authz.PrincipalFromPID(sender)
	return h.authorizer.Authorize(principal, receiver.GetName(), req.GetMethod())
}

func (h *Handler) RpcMessageHandler(sf inf.IRpcSenderFactory, req *actor.Message) error {
	if req == nil {
		return errors.New("rpc request is nil")
	}
	// proto 反序列化后同步 PID 的 MasterFlag 原子字段，避免 IsMasterNode() 读取到 false
	if p := req.GetSenderPid(); p != nil {
		p.SyncMasterFlag()
	}
	if p := req.GetReceiverPid(); p != nil {
		p.SyncMasterFlag()
	}

	headers := make(map[string]any, len(req.ContextHeaders)+2)
	for k, v := range req.ContextHeaders {
		headers[k] = v
	}

	if req.Reply {
		// 回复
		// 需要回复的信息都会加入monitor中,找到对应的信封数据
		rm := h.rpcMonitor
		if rm == nil {
			return nil
		}
		if state := rm.Remove(req.ReqId); state != nil {
			response, err := codec.DecodeFromAny(req.Response)
			if err == nil && len(req.Err) > 0 {
				err = errorx.UnmarshalFromBytes(req.Err)
			}

			state.SetResult(response, err)
			state.Complete()
			return nil
		} else {
			// 已经超时,丢弃返回
			// 迟到的回复：通常是调用方已超时/取消后的正常现象，避免刷屏按 debug 处理。
			if l := h.logger; l != nil {
				l.Debugf("rpc call late reply dropped (state not found): %s", req.String())
			}
			return nil
		}
	} else {
		// 去重只对“有ReqId”的请求有意义（通常是 NeedResp=true 的 call/asyncCall）。
		// fire-and-forget 的 send 使用 ReqId=0，跳过去重以减少热路径开销。
		if req.ReqId != 0 {
			senderPid := req.GetSenderPid()
			if senderPid == nil {
				return errors.New("rpc request sender pid is nil")
			}
			senderServiceUid := senderPid.GetServiceUid()
			// TODO 需要考虑GetRpcReqDuplicator这里在不同的节点中使用不同的模式,TTL或者LRU,防止在高并发节点在TTL模式下被瞬间击穿,会导致map容量爆炸式增加
			dedupIns := h.dedup
			if dedupIns == nil {
				return errors.New("deduplicator is nil")
			}
			if dedupIns.Seen(senderServiceUid, req.ReqId) {
				if l := h.logger; l != nil {
					l.Errorf("duplicate reqId:%d rpc request: %s", req.ReqId, req.String())
				}
				return nil
			}
		}
		// 授权检查：在 payload decode 之前拦截未授权请求，避免消耗反序列化成本
		if err := h.authorizeRequest(req); err != nil {
			if l := h.logger; l != nil {
				sender, receiver := "<nil>", "<nil>"
				if p := req.GetSenderPid(); p != nil {
					sender = p.GetServiceUid()
				}
				if p := req.GetReceiverPid(); p != nil {
					receiver = p.GetName()
				}
				l.Warnf("rpc authz denied: sender=%s receiver=%s method=%s reqId=%d err=%v",
					sender, receiver, req.GetMethod(), req.ReqId, err)
			}
			return err
		}
		if sf == nil {
			return errors.New("rpc sender factory is nil")
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

		dispatcher := sf.GetDispatcher(req.ReceiverPid)
		if dispatcher == nil {
			envelope.Release()
			return errors.New("rpc receiver dispatcher is nil")
		}
		err := dispatcher.DeliverRequest(ctx, envelope)
		if err != nil {
			envelope.Release()
			return err
		}

		return nil
	}
}

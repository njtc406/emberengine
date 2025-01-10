// Package rpc
// @Title  title
// @Description  desc
// @Author  yr  2024/11/5
// @Update  yr  2024/11/5
package rpc

import (
	"fmt"
	"github.com/njtc406/emberengine/engine/actor"
	"github.com/njtc406/emberengine/engine/errdef"
	"github.com/njtc406/emberengine/engine/inf"
	"github.com/njtc406/emberengine/engine/msgenvelope"
	"github.com/njtc406/emberengine/engine/utils/log"
	"reflect"
	"runtime/debug"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	apiPreFix = []string{
		"Api", "API",
	}

	rpcPreFix = []string{
		"Rpc", "RPC",
	}

	emptyError = reflect.TypeOf((*error)(nil))
)

type MethodInfo struct {
	Method   reflect.Method
	In       []reflect.Type
	Out      []reflect.Type
	MultiOut bool // 是否是多参数返回(排除error以外,还有两个及以上的返回值)
}

type Handler struct {
	inf.IRpcHandler

	methodMap map[string]*MethodInfo
	isPublic  bool // 是否是公开服务(有rpc调用的服务)
}

func (h *Handler) Init(rpcHandler inf.IRpcHandler) {
	h.IRpcHandler = rpcHandler
	h.methodMap = make(map[string]*MethodInfo)

	h.registerMethod()
}

func (h *Handler) registerMethod() {
	typ := reflect.TypeOf(h.IRpcHandler)
	for m := 0; m < typ.NumMethod(); m++ {
		method := typ.Method(m)
		err := h.suitableMethods(method)
		if err != nil {
			log.SysLogger.Panic(err)
		}
	}
}

func isExported(name string) bool {
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(r)
}

func hasPrefix(str string, ls []string) bool {
	for _, s := range ls {
		if strings.HasPrefix(str, s) {
			return true
		}
	}

	return false
}

func (h *Handler) isExportedOrBuiltinType(t reflect.Type) bool {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	// PkgPath will be non-empty even for an exported type,
	// so we need to check the type name as well.
	return isExported(t.Name()) || t.PkgPath() == ""
}

func (h *Handler) suitableMethods(method reflect.Method) error {
	// 只有以API或者rpc开头的方法才注册
	if !hasPrefix(method.Name, apiPreFix) {
		if !hasPrefix(method.Name, rpcPreFix) {
			// 不是API或者RPC开头的方法,直接返回
			return nil
		}

		if !h.isPublic {
			// 走到这说明有rpc方法,那么service即为公开服务,可以被远程调用
			h.isPublic = true
		}
	}

	var methodInfo MethodInfo

	// 判断参数类型,必须是其他地方可调用的
	var in []reflect.Type
	for i := 0; i < method.Type.NumIn(); i++ {
		if h.isExportedOrBuiltinType(method.Type.In(i)) == false {
			return fmt.Errorf("%s Unsupported parameter types", method.Name)
		}
		in = append(in, method.Type.In(i))
	}

	// TODO 如果是rpc方法,实际上最多只有一个入参和一个返回值(除去错误),看这里要不要校验一下,防止写错

	var outs []reflect.Type

	// 计算除了error,还有几个返回值
	var multiOut int

	for i := 0; i < method.Type.NumOut(); i++ {
		t := method.Type.Out(i)
		outs = append(outs, t)
		kd := t.Kind()
		if kd == reflect.Ptr || kd == reflect.Interface ||
			kd == reflect.Func || kd == reflect.Map ||
			kd == reflect.Slice || kd == reflect.Chan {
			if t.Implements(emptyError.Elem()) {
				continue
			} else {
				multiOut++
			}
		} else if t.Kind() == reflect.Struct {
			// 不允许直接使用结构体,只能给结构体指针
			return errdef.InputParamCantUseStruct
		} else {
			multiOut++
		}
	}

	if multiOut > 1 {
		methodInfo.MultiOut = true
	}

	name := method.Name
	methodInfo.In = in
	methodInfo.Method = method
	methodInfo.Out = outs
	h.methodMap[name] = &methodInfo
	return nil
}

func (h *Handler) HandleRequest(envelope inf.IEnvelope) {
	defer func() {
		if r := recover(); r != nil {
			log.SysLogger.Errorf("service[%s] handle message from caller: %s panic: %v\n trace:%s", h.GetName(), envelope.GetSenderPid().String(), r, debug.Stack())
			envelope.SetResponse(nil)
			envelope.SetError(errdef.HandleMessagePanic)
		}

		h.doResponse(envelope)
	}()

	//log.SysLogger.Debugf("rpc request handler -> begin handle message: %+v", envelope)

	var (
		params  []reflect.Value
		results []reflect.Value
		resp    []interface{}
	)
	methodInfo, ok := h.methodMap[envelope.GetMethod()]
	if !ok {
		envelope.SetError(errdef.MethodNotFound)
		return
	}

	params = append(params, reflect.ValueOf(h.GetRpcHandler()))
	if len(methodInfo.In) > 1 { // 需要排除第一个参数
		// 需要输入参数
		req := envelope.GetRequest()
		if req == nil {
			// 兼容入参给了nil
			for i := 1; i < len(methodInfo.In); i++ {
				params = append(params, reflect.Zero(methodInfo.In[i]))
			}
		} else {
			switch req.(type) {
			case []interface{}: // 支持本地调用时多参数
				for _, param := range req.([]interface{}) {
					params = append(params, reflect.ValueOf(param))
				}
			default:
				params = append(params, reflect.ValueOf(req))
			}
		}
	}

	if len(params) != len(methodInfo.In) {
		log.SysLogger.Errorf("method[%s] param count not match, need: %d  got: %d", envelope.GetMethod(), len(methodInfo.In), len(params))
		envelope.SetError(errdef.InputParamNotMatch)
		return
	}

	results = methodInfo.Method.Func.Call(params)
	if len(results) != len(methodInfo.Out) {
		// 这里应该不会触发,因为参数检查的时候已经做过了
		log.SysLogger.Errorf("method[%s] return value count not match", envelope.GetMethod())
		envelope.SetError(errdef.OutputParamNotMatch)
		return
	}

	if len(results) == 0 {
		// 没有返回值
		return
	}

	// 解析返回
	for i, t := range methodInfo.Out {
		result := results[i]
		if t.Kind() == reflect.Ptr ||
			t.Kind() == reflect.Interface ||
			t.Kind() == reflect.Func ||
			t.Kind() == reflect.Map ||
			t.Kind() == reflect.Slice ||
			t.Kind() == reflect.Chan {
			if t.Implements(reflect.TypeOf((*error)(nil)).Elem()) {
				if err, ok := result.Interface().(error); ok && err != nil {
					// 只要返回了错误,其他数据都不再接收
					envelope.SetError(err)
					return
				} else {
					continue
				}
			} else {
				var res interface{}
				if result.IsNil() {
					res = nil
				} else {
					res = result.Interface()
				}

				if methodInfo.MultiOut {
					resp = append(resp, res)
				} else {
					envelope.SetResponse(res)
				}
			}
		} else {
			res := result.Interface()

			if methodInfo.MultiOut {
				resp = append(resp, res)
			} else {
				envelope.SetResponse(res)
			}
		}
	}

	if methodInfo.MultiOut {
		// 兼容多返回参数
		envelope.SetResponse(resp)
	}
}

func (h *Handler) doResponse(envelope inf.IEnvelope) {
	if !envelope.IsRef() {
		// 已经被释放,丢弃
		return
	}
	if envelope.NeedResponse() {
		// 需要回复
		envelope.SetReply()      // 这是回复
		envelope.SetRequest(nil) // 清除请求数据

		// 发送回复信息
		if err := envelope.GetSender().SendResponse(envelope); err != nil {
			log.SysLogger.Errorf("service[%s] send response failed: %v", h.GetName(), err)
		}
	} else {
		// 不需要回复,释放资源
		msgenvelope.ReleaseMsgEnvelope(envelope)
	}
}

func (h *Handler) HandleResponse(envelope inf.IEnvelope) {
	defer func() {
		if r := recover(); r != nil {
			log.SysLogger.Errorf("service[%s] handle message panic: %v\n trace:%s", h.GetName(), r, debug.Stack())
		}
	}()

	// 执行回调
	envelope.RunCompletions()

	// 释放资源
	msgenvelope.ReleaseMsgEnvelope(envelope)
}

func (h *Handler) GetName() string {
	return h.IRpcHandler.GetName()
}

func (h *Handler) GetPid() *actor.PID {
	return h.IRpcHandler.GetPid()
}

func (h *Handler) GetRpcHandler() inf.IRpcHandler {
	return h.IRpcHandler
}

func (h *Handler) IsPrivate() bool {
	return !h.isPublic
}

func (h *Handler) IsClosed() bool {
	return h.IRpcHandler.IsClosed()
}

// Package rpc
// @Title  title
// @Description  desc
// @Author  yr  2024/11/5
// @Update  yr  2024/11/5
package rpc

import (
	"fmt"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"reflect"
	"runtime/debug"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

var (
	apiPreFix  = []string{"Api", "API"}
	rpcPreFix  = []string{"Rpc", "RPC"}
	emptyError = reflect.TypeOf((*error)(nil))
)

// MethodMgr 管理所有注册的方法
type MethodMgr struct {
	mu        sync.RWMutex
	rpcCnt    int // rpc 接口数量
	methodMap map[string]def.MethodCallFunc
}

func NewMethodMgr() inf.IMethodMgr {
	return &MethodMgr{
		methodMap: make(map[string]def.MethodCallFunc),
	}
}

func (m *MethodMgr) IsPrivate() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.rpcCnt == 0
}

func (m *MethodMgr) AddMethodFunc(name string, fn def.MethodCallFunc) {
	if name == "" {
		log.SysLogger.Debugf("method[%s] register failed", name)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if hasPrefix(name, rpcPreFix) {
		m.rpcCnt++
	}
	m.methodMap[name] = fn
}

func (m *MethodMgr) GetMethodFunc(name string) (def.MethodCallFunc, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	info, ok := m.methodMap[name]
	return info, ok
}

func (m *MethodMgr) RemoveMethods(names []string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	oldRpcCnt := m.rpcCnt
	for _, name := range names {
		delete(m.methodMap, name)
		if hasPrefix(name, rpcPreFix) {
			m.rpcCnt--
		}
		if m.rpcCnt < 0 {
			m.rpcCnt = 0
		}
	}

	// 只有一种情况会返回true,就是old>0&&now=0
	return oldRpcCnt > 0 && m.rpcCnt == 0
}

// Handler 用于处理 RPC 调用
type Handler struct {
	inf.IModule
	mgr     inf.IMethodMgr
	methods []string
}

func NewHandler(owner inf.IModule) *Handler {
	return &Handler{
		IModule: owner,
	}
}

func (h *Handler) Init(hd inf.IMethodMgr) inf.IRpcHandler {
	h.mgr = hd
	h.registerMethod()
	return h
}

func (h *Handler) registerMethod() {
	typ := reflect.TypeOf(h.IModule)
	for m := 0; m < typ.NumMethod(); m++ {
		err := h.suitableMethods(typ.Method(m))
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
	// 当类型名称为空时（例如内置类型），PkgPath 也为空
	return isExported(t.Name()) || t.PkgPath() == ""
}

func (h *Handler) suitableMethods(method reflect.Method) error {
	// 只注册以 Api 或 Rpc 开头的方法
	if !hasPrefix(method.Name, apiPreFix) && !hasPrefix(method.Name, rpcPreFix) {
		return nil
	}

	var in []reflect.Type
	for i := 0; i < method.Type.NumIn(); i++ {
		if !h.isExportedOrBuiltinType(method.Type.In(i)) {
			return fmt.Errorf("%s Unsupported parameter types", method.Name)
		}
		in = append(in, method.Type.In(i))
	}

	var outs []reflect.Type
	var multiOut int
	for i := 0; i < method.Type.NumOut(); i++ {
		t := method.Type.Out(i)
		outs = append(outs, t)
		kd := t.Kind()
		if kd == reflect.Ptr || kd == reflect.Interface || kd == reflect.Func ||
			kd == reflect.Map || kd == reflect.Slice || kd == reflect.Chan {
			if t.Implements(emptyError.Elem()) {
				continue
			} else {
				multiOut++
			}
		} else if t.Kind() == reflect.Struct {
			return def.ErrInputParamCantUseStruct
		} else {
			multiOut++
		}
	}

	name := method.Name
	// 预编译调用闭包，避免每次调用都走反射的全流程
	h.mgr.AddMethodFunc(name, compileCallFunc(reflect.ValueOf(h.IModule), name, method.Func, in, outs, multiOut > 1, method.Type.IsVariadic()))
	h.methods = append(h.methods, name)
	log.SysLogger.Debugf("service[%s] method[%s] register success", h.GetModuleName(), name)
	return nil
}

// compileCallFunc 预编译调用闭包
func compileCallFunc(owner reflect.Value, name string, methodFunc reflect.Value, in, outs []reflect.Type, multiOut, isVariadic bool) func(req interface{}) (interface{}, error) {
	paramCount := len(in)
	return func(req interface{}) (interface{}, error) {
		params := []reflect.Value{owner}

		// 处理参数
		if isVariadic {
			var fixedCount int
			if paramCount > 1 {
				fixedCount = paramCount - 2 // 计算固定参数数量（排除接收者和可变参数）
			} else {
				fixedCount = 0
			}

			if req == nil {
				if fixedCount > 0 {
					log.SysLogger.Errorf("method[%s] param count not match, need at least: %d, got: 0", name, fixedCount)
					return nil, def.ErrInputParamNotMatch
				}
			} else {
				if reqSlice, ok := req.([]interface{}); ok {
					if len(reqSlice) < fixedCount {
						log.SysLogger.Errorf("method[%s] param count not match, need at least: %d, got: %d", name, fixedCount, len(reqSlice))
						return nil, def.ErrInputParamNotMatch
					}

					// 如果请求参数有多个,直接添加到参数列表中
					for i := 0; i < len(reqSlice); i++ {
						params = append(params, reflect.ValueOf(reqSlice[i]))
					}
				} else {
					if fixedCount > 0 {
						// 只有一个可变参,就不允许有多个参数
						log.SysLogger.Errorf("method[%s] param count not match", name)
						return nil, def.ErrInputParamNotMatch
					}
					// 否则只有一个参数
					params = append(params, reflect.ValueOf(req))
				}
			}
		} else {
			// 非 variadic 方法处理
			if req == nil {
				if paramCount != 1 {
					log.SysLogger.Errorf("method[%s] param count not match, need : %d, got: 0", name, paramCount-1)
					return nil, def.ErrInputParamNotMatch
				}
			} else {
				switch reqData := req.(type) {
				case []interface{}:
					if len(reqData) != paramCount-1 {
						log.SysLogger.Errorf("method[%s] param count not match, need: %d, got: %d", name, paramCount-1, len(reqData))
						return nil, def.ErrInputParamNotMatch
					}
					for i := 0; i < len(reqData); i++ {
						params = append(params, reflect.ValueOf(reqData[i]))
					}
				default:
					if paramCount != 2 {
						log.SysLogger.Errorf("method[%s] param count not match", name)
						return nil, def.ErrInputParamNotMatch
					}
					params = append(params, reflect.ValueOf(req))
				}
			}
		}

		results := methodFunc.Call(params)

		// 处理返回值
		if len(results) == 0 {
			return nil, nil
		}

		var output []interface{}
		for i, t := range outs {
			result := results[i]
			if t.Implements(reflect.TypeOf((*error)(nil)).Elem()) {
				if !result.IsNil() {
					return nil, result.Interface().(error)
				}
			} else {
				if multiOut {
					output = append(output, result.Interface())
				} else {
					return result.Interface(), nil
				}
			}
		}

		return output, nil
	}
}

func (h *Handler) HandleRequest(envelope inf.IEnvelope) {
	meta := envelope.GetMeta()
	data := envelope.GetData()
	defer func() {
		if r := recover(); r != nil {
			log.SysLogger.Errorf("service[%s] handle message from caller: %s panic: %v\n trace:%s",
				h.GetModuleName(), meta.GetSenderPid().String(), r, debug.Stack())
			data.SetResponse(nil)
			data.SetError(def.ErrHandleMessagePanic)
		}
		h.doResponse(envelope)
	}()
	call, ok := h.mgr.GetMethodFunc(data.GetMethod())
	if !ok {
		data.SetError(def.ErrMethodNotFound)
		return
	}
	resp, err := call(data.GetRequest())
	if err != nil {
		log.SysLogger.WithContext(envelope.GetContext()).Errorf("method call failed:%v", err)
		data.SetError(err)
		return
	}
	data.SetResponse(resp)
}

func (h *Handler) doResponse(envelope inf.IEnvelope) {
	if !envelope.IsRef() {
		return
	}
	meta := envelope.GetMeta()
	data := envelope.GetData()
	if data.NeedResponse() {
		data.SetReply()
		data.SetRequest(nil)

		// 将receiver设置为sender,防止nats那里找不到对应的topic
		sender := meta.GetSenderPid()
		meta.SetReceiverPid(sender)
		if err := meta.GetDispatcher().SendResponse(envelope); err != nil {
			log.SysLogger.WithContext(envelope.GetContext()).Errorf("service[%s] send response failed: %v", h.GetModuleName(), err)
		}
	} else {
		envelope.Release()
	}
}

func (h *Handler) HandleResponse(envelope inf.IEnvelope) {
	defer func() {
		if r := recover(); r != nil {
			log.SysLogger.Errorf("service[%s] handle message panic: %v\n trace:%s",
				h.GetModuleName(), r, debug.Stack())
		}

		envelope.Release()
	}()

	envelope.RunCompletions()
}

func (h *Handler) GetMethods() []string {
	return h.methods
}

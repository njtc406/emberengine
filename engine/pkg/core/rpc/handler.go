// Package rpc
// @Title  title
// @Description  desc
// @Author  yr  2024/11/5
// @Update  yr  2024/11/5
package rpc

import (
	"fmt"
	"github.com/njtc406/emberengine/engine/internal/message/msgenvelope"
	"reflect"
	"runtime/debug"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
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

type MethodMgr struct {
	mu        sync.RWMutex
	rpcCnt    int // rpc接口数量
	methodMap map[string]*def.MethodInfo
}

func NewMethodMgr() inf.IMethodMgr {
	return &MethodMgr{
		methodMap: make(map[string]*def.MethodInfo),
	}
}

func (m *MethodMgr) IsPrivate() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	//log.SysLogger.Debugf("method num: %d", m.rpcCnt)
	return m.rpcCnt == 0
}

func (m *MethodMgr) AddMethod(name string, info *def.MethodInfo) {
	if name == "" {
		log.SysLogger.Debugf("method[%s] register failed", name)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if hasPrefix(name, rpcPreFix) {
		m.rpcCnt++
	}
	m.methodMap[name] = info
}

func (m *MethodMgr) GetMethod(name string) (*def.MethodInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	info, ok := m.methodMap[name]
	return info, ok
}

func (m *MethodMgr) RemoveMethods(names []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, name := range names {
		delete(m.methodMap, name)
		if hasPrefix(name, rpcPreFix) {
			m.rpcCnt--
		}
		if m.rpcCnt < 0 {
			m.rpcCnt = 0
		}
	}
}

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
	// PkgPath will be non-empty even for an exported type,
	// so we need to check the type name as well.
	return isExported(t.Name()) || t.PkgPath() == ""
}

func (h *Handler) suitableMethods(method reflect.Method) error {
	// 只有以API或者rpc开头的方法才注册
	//log.SysLogger.Debugf("service[%s] method[%s] register begin", h.GetModuleName(), method.Name)
	if !hasPrefix(method.Name, apiPreFix) {
		if !hasPrefix(method.Name, rpcPreFix) {
			// 不是API或者RPC开头的方法,直接返回
			return nil
		}
	}

	var methodInfo def.MethodInfo

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
			return def.InputParamCantUseStruct
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
	methodInfo.Handler = reflect.ValueOf(h.IModule)
	h.mgr.AddMethod(name, &methodInfo)
	h.methods = append(h.methods, name)
	log.SysLogger.Debugf("service[%s] method[%s] register success", h.GetModuleName(), name)
	return nil
}

func (h *Handler) HandleRequest(envelope inf.IEnvelope) {
	defer func() {
		if r := recover(); r != nil {
			log.SysLogger.Errorf("service[%s] handle message from caller: %s panic: %v\n trace:%s", h.GetModuleName(), envelope.GetSenderPid().String(), r, debug.Stack())
			envelope.SetResponse(nil)
			envelope.SetError(def.HandleMessagePanic)
		}

		h.doResponse(envelope)
	}()

	//log.SysLogger.Debugf("rpc request mgr -> begin handle message: %+v", envelope)

	var (
		params  []reflect.Value
		results []reflect.Value
		resp    []interface{}
	)
	methodInfo, ok := h.mgr.GetMethod(envelope.GetMethod())
	if !ok {
		envelope.SetError(def.MethodNotFound)
		return
	}

	params = append(params, methodInfo.Handler)

	// 判断是否有可变参
	isVariadic := methodInfo.Method.Type.IsVariadic()

	req := envelope.GetRequest()
	if isVariadic {
		var fixedCount int
		if len(methodInfo.In) > 1 {
			fixedCount = len(methodInfo.In) - 2 // 排除接收者和可变参
		} else {
			fixedCount = 0
		}
		// 可变参方法的处理
		if req == nil {
			// 没有传入参数时，固定参数（如果有）使用零值，variadic参数打包成空 slice
			for i := 1; i <= fixedCount; i++ {
				params = append(params, reflect.Zero(methodInfo.In[i]))
			}
			variadicType := methodInfo.In[len(methodInfo.In)-1].Elem()
			emptySlice := reflect.MakeSlice(reflect.SliceOf(variadicType), 0, 0)
			params = append(params, emptySlice)
		} else {
			// 请求参数可以是 []interface{} 或其他单个值
			if reqSlice, ok := req.([]interface{}); ok {
				// 如果固定参数不为零，则取前 fixedCount 个作为固定参数
				if len(reqSlice) < fixedCount {
					log.SysLogger.Errorf("method[%s] param count not match, need at least: %d  got: %d",
						envelope.GetMethod(), fixedCount, len(reqSlice))
					envelope.SetError(def.InputParamNotMatch)
					return
				}
				// 添加固定参数
				for i := 0; i < fixedCount; i++ {
					params = append(params, reflect.ValueOf(reqSlice[i]))
				}
				// 剩下的全部归为 variadic 参数
				variadicType := methodInfo.In[len(methodInfo.In)-1].Elem()
				sliceVal := reflect.MakeSlice(reflect.SliceOf(variadicType), 0, len(reqSlice)-fixedCount)
				for i := fixedCount; i < len(reqSlice); i++ {
					sliceVal = reflect.Append(sliceVal, reflect.ValueOf(reqSlice[i]))
				}
				params = append(params, sliceVal)
			} else {
				// 如果 req 不是 slice，则认为只有 variadic参数，并打包成单元素 slice
				if fixedCount > 0 {
					// 如果方法定义有固定参数而 req 不是 slice，就报错
					log.SysLogger.Errorf("method[%s] param count not match", envelope.GetMethod())
					envelope.SetError(def.InputParamNotMatch)
					return
				}
				variadicType := methodInfo.In[len(methodInfo.In)-1].Elem()
				sliceVal := reflect.MakeSlice(reflect.SliceOf(variadicType), 0, 1)
				sliceVal = reflect.Append(sliceVal, reflect.ValueOf(req))
				params = append(params, sliceVal)
			}
		}
	} else {
		// 非 variadic 方法处理
		if req == nil {
			for i := 1; i < len(methodInfo.In); i++ {
				params = append(params, reflect.Zero(methodInfo.In[i]))
			}
		} else {
			switch req.(type) {
			case []interface{}:
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
		envelope.SetError(def.InputParamNotMatch)
		return
	}

	results = methodInfo.Method.Func.Call(params)
	if len(results) != len(methodInfo.Out) {
		// 这里应该不会触发,因为参数检查的时候已经做过了
		log.SysLogger.Errorf("method[%s] return value count not match", envelope.GetMethod())
		envelope.SetError(def.OutputParamNotMatch)
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
		if err := envelope.GetDispatcher().SendResponse(envelope); err != nil {
			log.SysLogger.Errorf("service[%s] send response failed: %v", h.GetModuleName(), err)
		}
	} else {
		// 不需要回复,释放资源
		msgenvelope.ReleaseMsgEnvelope(envelope)
	}
}

func (h *Handler) HandleResponse(envelope inf.IEnvelope) {
	defer func() {
		if r := recover(); r != nil {
			log.SysLogger.Errorf("service[%s] handle message panic: %v\n trace:%s", h.GetModuleName(), r, debug.Stack())
		}
	}()

	// 执行回调
	envelope.RunCompletions()

	// 释放资源
	msgenvelope.ReleaseMsgEnvelope(envelope)
}

func (h *Handler) GetMethods() []string {
	return h.methods
}

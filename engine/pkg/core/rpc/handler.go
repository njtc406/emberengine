// Package rpc
// @Title  title
// @Description  desc
// @Author  yr  2024/11/5
// @Update  yr  2024/11/5
package rpc

import (
	"context"
	"fmt"
	"reflect"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/rpc/message/msgenvelope"
)

var emptyError = reflect.TypeOf((*error)(nil))

// MethodMgr 管理所有注册的方法
type MethodMgr struct {
	mu          sync.RWMutex
	rpcCnt      int
	methodMap   map[string]def.MethodCallFunc
	readOnlyMap map[string]bool // 方法名 → 是否只读
	enableRW    *atomic.Bool    // 引用 WorkerPool 的 enableRW，用于 RemoveMethods 防御检查
	logger      log.ILoggerX
}

func NewMethodMgr(logger log.ILoggerX) inf.IMethodMgr {
	return &MethodMgr{
		methodMap:   make(map[string]def.MethodCallFunc),
		readOnlyMap: make(map[string]bool),
		logger:      logger,
	}
}

// SetEnableRW 设置 enableRW 引用，用于 RemoveMethods 防御检查
func (m *MethodMgr) SetEnableRW(flag *atomic.Bool) {
	m.enableRW = flag
}

func (m *MethodMgr) IsPrivate() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.rpcCnt == 0
}

func (m *MethodMgr) AddMethodFunc(name string, fn def.MethodCallFunc) {
	if name == "" {
		m.logger.Debugf("method[%s] register failed", name)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// RpcRo/RPCRo 前缀是 Rpc/RPC 的超集，先检查 ReadOnly 前缀，短路防重复计数
	if hasRpcReadOnlyPrefix(name) || hasRpcPrefix(name) {
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
	// 【防御性校验】RW 模式下，运行期 GetMethodFunc()/IsReadOnly() 并发读 methodMap/readOnlyMap，
	// 如果此时 RemoveMethods 写入 map → map concurrent read/write fatal。
	// 正常调用时机是 shutdown 阶段（Worker 已停止），此校验防止误用。
	if m.enableRW != nil && m.enableRW.Load() {
		m.logger.Errorf("RemoveMethods called while RW mode is active! "+
			"This may cause data race. Caller should ensure all Workers are stopped. names=%v", names)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	oldRpcCnt := m.rpcCnt
	for _, name := range names {
		if _, ok := m.methodMap[name]; !ok {
			continue
		}
		delete(m.methodMap, name)
		delete(m.readOnlyMap, name)
		// RpcRo/RPCRo 前缀是 Rpc/RPC 的超集，与 AddMethodFunc 保持一致（仅 Rpc 前缀计入 rpcCnt）
		if hasRpcReadOnlyPrefix(name) || hasRpcPrefix(name) {
			m.rpcCnt--
		}
		if m.rpcCnt < 0 {
			m.rpcCnt = 0
		}
	}

	// 只有一种情况会返回true,就是old>0&&now=0
	return oldRpcCnt > 0 && m.rpcCnt == 0
}

// MarkReadOnly 标记指定方法为只读（启动阶段调用，供 IReadOnlyDeclarer 批量设置）
// 实现 IReadOnlyMethodMgr 接口
func (m *MethodMgr) MarkReadOnly(name string) {
	m.readOnlyMap[name] = true
}

// IsReadOnly 查询方法是否为只读（无锁，运行期只读的静态表）
// 实现 IReadOnlyMethodMgr 接口
func (m *MethodMgr) IsReadOnly(name string) bool {
	return m.readOnlyMap[name]
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
			h.Panic(err)
		}
	}

	// 扫描完所有方法后，检查模块是否实现 IReadOnlyDeclarer，补充手动声明
	if declarer, ok := h.IModule.(inf.IReadOnlyDeclarer); ok {
		if roMgr, ok := h.mgr.(inf.IReadOnlyMethodMgr); ok {
			for _, name := range declarer.ReadOnlyMethods() {
				// 冲突检测：如果方法前缀不是 ReadOnly（如 Rpc/Api 前缀，无 Ro），
				// 但 IReadOnlyDeclarer 却将其声明为 ReadOnly，输出警告
				if !hasRpcReadOnlyPrefix(name) && !hasApiReadOnlyPrefix(name) &&
					(hasRpcPrefix(name) || hasApiPrefix(name)) {
					h.Warnf("Method '%s' has a write-style prefix (Rpc/Api) but is declared "+
						"as ReadOnly by IReadOnlyDeclarer. Please verify this is intentional. "+
						"If this method modifies state, it should NOT be in ReadOnlyMethods().", name)
				}
				roMgr.MarkReadOnly(name)
			}
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
	// ReadOnly 前缀是普通前缀的超集（RpcRo 包含 Rpc，ApiRo 包含 Api），先检查 ReadOnly
	isReadOnly := hasApiReadOnlyPrefix(method.Name) || hasRpcReadOnlyPrefix(method.Name)
	if !isReadOnly && !hasApiPrefix(method.Name) && !hasRpcPrefix(method.Name) {
		return nil
	}

	var in []reflect.Type
	for i := 0; i < method.Type.NumIn(); i++ {
		if !h.isExportedOrBuiltinType(method.Type.In(i)) {
			return fmt.Errorf("%s Unsupported parameter types", method.Name)
		}
		in = append(in, method.Type.In(i))
	}

	ctxType := reflect.TypeOf((*context.Context)(nil)).Elem()
	hasCtx := false

	if len(in) > 1 && in[1] == ctxType {
		hasCtx = true
		// 剔除 ctx：保留 receiver(in[0])，去掉 in[1]，其余顺延
		in = append([]reflect.Type{in[0]}, in[2:]...)
	} else {
		// 如果 ctx 出现在其他位置，认为非法
		for idx := 2; idx < len(in); idx++ {
			if in[idx] == ctxType {
				return fmt.Errorf("%s invalid signature: context.Context must be the first parameter after receiver", method.Name)
			}
		}
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
	h.mgr.AddMethodFunc(
		name,
		compileCallFunc(
			reflect.ValueOf(h.IModule),
			name,
			method.Func,
			in,
			outs,
			multiOut > 1,
			method.Type.IsVariadic(),
			hasCtx, // 新增：告诉闭包是否需要自动注入 ctx
			h,
		),
	)
	// 如果是 ReadOnly 前缀，标记为只读方法 TODO 只读方法中不允许修改任何数据,这里需要想办法限制一下内部，如果是调用了只读方法，最后不执行commit
	if isReadOnly {
		if roMgr, ok := h.mgr.(inf.IReadOnlyMethodMgr); ok {
			roMgr.MarkReadOnly(name)
		}
	}

	h.methods = append(h.methods, name)
	h.Debugf("method[%s] register success, readOnly=%v", name, isReadOnly)
	return nil
}

// compileCallFunc 预编译调用闭包
func compileCallFunc(owner reflect.Value, name string, methodFunc reflect.Value, in, outs []reflect.Type, multiOut, isVariadic bool, hasCtx bool, logger log.ILoggerX) func(ctx context.Context, req interface{}) (interface{}, error) {
	paramCount := len(in)
	return func(ctx context.Context, req interface{}) (interface{}, error) {
		params := []reflect.Value{owner}

		if hasCtx {
			params = append(params, reflect.ValueOf(ctx))
		}

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
					logger.Errorf("method[%s] param count not match, need at least: %d, got: 0    params:%+v", name, fixedCount, params)
					return nil, def.ErrInputParamNotMatch
				}
			} else {
				if reqSlice, ok := req.([]interface{}); ok {
					if len(reqSlice) < fixedCount {
						logger.Errorf("method[%s] param count not match, need at least: %d, got: %d     params:%+v", name, fixedCount, len(reqSlice), params)
						return nil, def.ErrInputParamNotMatch
					}

					// 如果请求参数有多个,直接添加到参数列表中
					for i := 0; i < len(reqSlice); i++ {
						params = append(params, reflect.ValueOf(reqSlice[i]))
					}
				} else {
					if fixedCount > 0 {
						// 只有一个可变参,就不允许有多个参数
						logger.Errorf("method[%s] param count not match", name)
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
					logger.Errorf("method[%s] param count not match, need : %d, got: 0    params:%+v", name, paramCount-1, params)
					return nil, def.ErrInputParamNotMatch
				}
			} else {
				switch reqData := req.(type) {
				case []interface{}:
					if len(reqData) != paramCount-1 {
						logger.Errorf("method[%s] param count not match, need: %d, got: %d     params:%+v", name, paramCount-1, len(reqData), params)
						return nil, def.ErrInputParamNotMatch
					}
					for i := 0; i < len(reqData); i++ {
						params = append(params, reflect.ValueOf(reqData[i]))
					}
				default:
					if paramCount != 2 {
						logger.Errorf("method[%s] param count not match", name)
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

func (h *Handler) HandleRequest(ctx context.Context, envelope inf.IEnvelope) error {
	meta := envelope.GetMeta()
	data := envelope.GetData()
	defer func() {
		if r := recover(); r != nil {
			h.WithContext(ctx).
				WithField("caller", meta.GetSenderPid().String()).
				WithField("method", data.GetMethod()).
				WithField("error", r).
				Error("handle request err")
			data.SetResponse(nil)
			data.SetError(def.ErrHandleMessagePanic)
		}
		h.doResponse(ctx, envelope)
	}()

	call, ok := h.mgr.GetMethodFunc(data.GetMethod())
	if !ok {
		data.SetError(def.ErrMethodNotFound)
		return nil
	}
	resp, err := call(ctx, data.GetRequest())
	if err != nil {
		h.WithContext(ctx).Errorf("method call failed:%v", err)
		data.SetError(err)
		return nil
	}
	data.SetResponse(resp)
	return nil
}

func (h *Handler) doResponse(ctx context.Context, envelope inf.IEnvelope) {
	data := envelope.GetData()
	meta := envelope.GetMeta()
	if data == nil || meta == nil {
		return
	}
	if !data.NeedResponse() {
		return
	}

	dispatcher := meta.GetDispatcher()
	if dispatcher == nil {
		h.WithContext(ctx).Errorf("service[%s] send response failed: dispatcher is nil", h.GetModuleName())
		return
	}

	// 回复不能复用“当前正在处理的请求 envelope”。
	// 原 envelope 的生命周期由接收方 mailbox 管理；若这里复用并走远端 sender（其内部会 Release），
	// 会导致请求 envelope 过早回收到池里，引发并发复用污染（Request 丢失 / meta,data=nil 等）。
	respEnv := msgenvelope.NewMsgEnvelope()

	respData := msgenvelope.NewData()
	respData.SetMethod(data.GetMethod())
	respData.SetReply()
	respData.SetRequest(nil)
	respData.SetResponse(data.GetResponse())
	respData.SetError(data.GetError())
	respData.SetNeedResponse(false)
	respEnv.SetData(respData)

	respMeta := msgenvelope.NewMeta()
	respMeta.SetReqId(meta.GetReqId())
	respMeta.SetSenderPid(meta.GetReceiverPid())
	respMeta.SetReceiverPid(meta.GetSenderPid())
	respMeta.SetDispatcher(dispatcher)
	respEnv.SetMeta(respMeta)

	if err := dispatcher.DeliverResponse(ctx, respEnv); err != nil {
		h.WithContext(ctx).Errorf("service[%s] send response failed: %v", h.GetModuleName(), err)
		respEnv.Release()
	}
}

func (h *Handler) HandleResponse(ctx context.Context, envelope inf.IEnvelope) error {
	defer func() {
		if r := recover(); r != nil {
			h.WithContext(ctx).Errorf("service[%s] handle message panic: %v\n trace:%s",
				h.GetModuleName(), r, debug.Stack())
		}
	}()

	meta := envelope.GetMeta()
	data := envelope.GetData()
	if meta == nil || data == nil {
		return nil
	}

	cb, params := meta.GetCallback()
	if cb == nil {
		return nil
	}

	cb.DoCallback(ctx, data.GetResponse(), data.GetError(), params...)

	return nil
}

func (h *Handler) GetMethods() []string {
	return h.methods
}

// Package msgenvelope
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/13 0013 22:22
// 最后更新:  yr  2025/7/13 0013 22:22
package msgenvelope

import (
	"errors"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/serializer"
	"sync"
)

func NewData() inf.IEnvelopeData {
	return &Data{} // data不使用缓存池, 因为多个共用相同的data时,前面的释放会导致后面的都取不到了
}

type Data struct {
	locker sync.RWMutex
	// 数据包
	method      string      // 调用方法
	reply       bool        // 是否是回复
	request     interface{} // 请求参数
	response    interface{} // 回复数据
	needResp    bool        // 是否需要回复
	err         error       // 错误
	requestBuff []byte      // 编码好的数据
	typeName    string      // 类型名
}

func (e *Data) Reset() {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.method = ""
	e.reply = false
	e.request = nil
	e.response = nil
	e.needResp = false
	e.err = nil
	e.requestBuff = e.requestBuff[:0]
	e.typeName = ""
}

func (e *Data) SetMethod(method string) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.method = method
}

func (e *Data) SetReply() {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.reply = true
}

func (e *Data) SetRequest(req interface{}) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.request = req
}

func (e *Data) SetResponse(res interface{}) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.response = res
}

func (e *Data) SetError(err error) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.err = err
}

func (e *Data) SetErrStr(err string) {
	e.locker.Lock()
	defer e.locker.Unlock()
	if err == "" {
		e.err = nil
		return
	}
	e.err = errors.New(err)
}

func (e *Data) SetNeedResponse(need bool) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.needResp = need
}

func (e *Data) SetRequestBuff(reqBuff []byte) {
	e.locker.Lock()
	defer e.locker.Unlock()
	e.requestBuff = reqBuff
}

func (e *Data) GetMethod() string {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.method
}

func (e *Data) GetRequest() interface{} {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.request
}

func (e *Data) GetResponse() interface{} {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.response
}

func (e *Data) GetError() error {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.err
}

func (e *Data) GetErrStr() string {
	e.locker.RLock()
	defer e.locker.RUnlock()
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *Data) GetRequestBuff(tpId int32) ([]byte, string, error) {
	e.locker.Lock()
	defer e.locker.Unlock()

	if e.request == nil {
		return nil, "", nil
	}

	if len(e.requestBuff) > 0 {
		return e.requestBuff, e.typeName, e.err
	}

	e.requestBuff, e.typeName, e.err = serializer.Serialize(e.request, tpId)

	return e.requestBuff, e.typeName, e.err
}

func (e *Data) IsReply() bool {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.reply
}

func (e *Data) NeedResponse() bool {
	e.locker.RLock()
	defer e.locker.RUnlock()
	return e.needResp
}

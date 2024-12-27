// Package inf
// @Title 调用监控
// @Description  用于监控一些具有超时属性的调用
// @Author  yr  2024/9/4 下午7:49
// @Update  yr  2024/9/4 下午7:49
package inf

type IMonitor interface {
	Init() IMonitor
	Start()
	Stop()
	Add(call IEnvelope)
	Remove(seq uint64) IEnvelope
	Get(seq uint64) IEnvelope
	GenSeq() uint64
}

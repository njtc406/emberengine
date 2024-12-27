// Package inf
// @Title  title
// @Description  desc
// @Author  yr  2024/11/26
// @Update  yr  2024/11/26
package inf

type IDiscovery interface {
	Init(eventProcessor IEventProcessor) error
	Start()
	Close()
}

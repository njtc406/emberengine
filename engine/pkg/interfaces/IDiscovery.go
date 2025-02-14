// Package interfaces
// @Title  title
// @Description  desc
// @Author  yr  2024/11/26
// @Update  yr  2024/11/26
package interfaces

type IDiscovery interface {
	Init(eventProcessor IEventProcessor) error
	Start()
	Close()
}

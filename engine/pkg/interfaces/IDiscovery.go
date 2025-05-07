// Package interfaces
// @Title  title
// @Description  desc
// @Author  yr  2024/11/26
// @Update  yr  2024/11/26
package interfaces

import "github.com/njtc406/emberengine/engine/pkg/config"

type IDiscovery interface {
	Init(eventProcessor IEventProcessor, conf *config.ClusterConf) error
	Start()
	Close()
}

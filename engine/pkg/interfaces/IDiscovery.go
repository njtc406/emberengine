// Package interfaces
// @Title  title
// @Description  desc
// @Author  yr  2024/11/26
// @Update  yr  2024/11/26
package interfaces

import (
	"github.com/njtc406/emberengine/engine/pkg/config"
)

type IDiscovery interface {
	Init(conf *config.ClusterConf, eventProcessor IEventProcessor, evtCh IEventChannel) error
	Start()
	Close()
}

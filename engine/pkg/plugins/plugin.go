// Package plugins
// @Title  title
// @Description  desc
// @Author  yr  2024/11/19
// @Update  yr  2024/11/19
package plugins

import (
	"sync"

	"github.com/njtc406/emberengine/engine/pkg/config"
)

// TODO 这个文件后面再做,现在还没有什么好的思路

type PluginInfo struct {
	Path string
	Name string
	Conf *config.ServiceConfig
}

type PluginManager struct {
	lock      sync.Mutex
	pluginMap map[string]*PluginInfo
}

func NewPluginManager() *PluginManager {
	return &PluginManager{pluginMap: make(map[string]*PluginInfo)}
}

func (pm *PluginManager) Register(name string, path string) {
	pm.lock.Lock()
	defer pm.lock.Unlock()
	pm.pluginMap[name] = &PluginInfo{Path: path, Name: name}
}

func (pm *PluginManager) LoadAll() {
	pm.lock.Lock()
	for _, v := range pm.pluginMap {
		load(v)
	}
	pm.lock.Unlock()
}

func load(plugin *PluginInfo) {

}

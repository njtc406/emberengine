// Package plugins
// @Title  title
// @Description  desc
// @Author  yr  2024/11/19
// @Update  yr  2024/11/19
package plugins

import (
	"github.com/njtc406/emberengine/engine/def"
	"sync"
)

// TODO 这个文件后面再做,现在还没有什么好的思路

var pluginMap = make(map[string]*PluginInfo)
var lock sync.Mutex

type PluginInfo struct {
	Path string
	Name string
	Conf *def.ServiceConfig
}

func Register(name string, path string) {
	lock.Lock()
	defer lock.Unlock()
	pluginMap[name] = &PluginInfo{Path: path, Name: name}
}

func LoadAll() {
	lock.Lock()
	for _, v := range pluginMap {
		load(v)
	}
	lock.Unlock()
}

func load(plugin *PluginInfo) {

}

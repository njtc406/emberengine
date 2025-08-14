// Package router_center
// 模块名: 路由中心
// 功能描述: 描述
// 作者:  yr  2024/1/4 0004 1:57
// 最后更新:  yr  2024/1/4 0004 1:57
package router_center

import (
	"github.com/gin-gonic/gin"
	"sync"
)

type Handler func(engine *gin.RouterGroup)
type StaticHandler func(engine *gin.Engine, group string)

const (
	DefaultGroup = `/api/v1`
)

type GroupHandlerPool struct {
	lock       sync.RWMutex
	pool       map[string][]Handler
	staticPool map[string][]StaticHandler
}

func NewGroupHandlerPool() *GroupHandlerPool {
	return &GroupHandlerPool{
		pool:       make(map[string][]Handler),
		staticPool: make(map[string][]StaticHandler),
	}
}

// RouteSet 设置路由
func (ghp *GroupHandlerPool) RouteSet(e *gin.Engine) {
	ghp.lock.RLock()
	defer ghp.lock.RUnlock()
	for group, pool := range ghp.pool {
		g := e.Group(group)
		for _, hd := range pool {
			hd(g)
		}
	}
	for group, pool := range ghp.staticPool {
		for _, hd := range pool {
			hd(e, group)
		}
	}
}

// RegisterGroupHandler 注册路由
func (ghp *GroupHandlerPool) RegisterGroupHandler(group string, hd Handler) {
	ghp.lock.Lock()
	defer ghp.lock.Unlock()
	_, ok := ghp.pool[group]
	if !ok {
		ghp.pool[group] = []Handler{}
	}

	ghp.pool[group] = append(ghp.pool[group], hd)
}

// RegisterStaticGroupHandler 注册静态路由
func (ghp *GroupHandlerPool) RegisterStaticGroupHandler(group string, hd StaticHandler) {
	ghp.lock.Lock()
	defer ghp.lock.Unlock()
	_, ok := ghp.staticPool[group]
	if !ok {
		ghp.staticPool[group] = []StaticHandler{}
	}

	ghp.staticPool[group] = append(ghp.staticPool[group], hd)
}

// Package adapter_ws
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/8/14 0014 0:38
// 最后更新:  yr  2025/8/14 0014 0:38
package ws

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	glbConfig "github.com/njtc406/emberengine/engine/pkg/config"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/sysService/gate/config"
	"github.com/njtc406/emberengine/engine/pkg/sysService/gate/protocol_adapter/connx"
	"github.com/njtc406/emberengine/engine/pkg/utils/httpx"
	"github.com/njtc406/emberengine/engine/pkg/utils/httpx/router_center"
	"net/http"
)

type WebSocketAdapter struct {
	server     *httpx.GinServer
	svc        inf.IService
	sessionMgr inf.ISessionManager
}

func NewWebSocketAdapter() *WebSocketAdapter {
	return &WebSocketAdapter{
		server: httpx.NewGinServer(),
	}
}

func (w *WebSocketAdapter) SetSessionMgr(sessionMgr inf.ISessionManager) {
	w.sessionMgr = sessionMgr
}

func (w *WebSocketAdapter) GetSessionMgr() inf.ISessionManager {
	return w.sessionMgr
}

func (w *WebSocketAdapter) ListenAndServe(svc inf.IService, conf interface{}) error {
	w.svc = svc
	cfg, ok := conf.(*config.WSServerConf)
	if !ok {
		return fmt.Errorf("invalid websocket configuration")
	}

	if err := w.server.Init(svc.GetLogger(), glbConfig.GetStatus(), cfg.HttpConf); err != nil {
		return err
	}
	pool := router_center.NewGroupHandlerPool()
	pool.RegisterGroupHandler(cfg.Router, w.router)
	w.server.SetRouter(pool)
	w.server.WithMiddleware(w.Auth)
	w.server.Start()
	return nil
}

func (w *WebSocketAdapter) router(rg *gin.RouterGroup) {
	upGrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true // 允许跨域
		},
	}

	rg.GET("", func(gc *gin.Context) {
		c, err := upGrader.Upgrade(gc.Writer, gc.Request, nil)
		if err != nil {
			gc.String(http.StatusBadRequest, "upgrade failed: %v", err)
			return
		}
		// 鉴权在中间件的时候就已经执行了,所以这里可以直接等同于连接成功,开始正常执行逻辑
		uid := gc.GetString("uid")
		conn := connx.NewWSConn(c)
		w.sessionMgr.Bind(uid, conn)
	})
}

func (w *WebSocketAdapter) Auth(c *gin.Context) {
	// TODO 鉴权
}

func (w *WebSocketAdapter) Shutdown(ctx context.Context) error {
	w.server.Stop()
	return nil
}

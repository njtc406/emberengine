// Package adapter_ws
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/8/14 0014 0:38
// 最后更新:  yr  2025/8/14 0014 0:38
package ws

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	glbConfig "github.com/njtc406/emberengine/engine/pkg/config"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/sysModule/gate/config"
	"github.com/njtc406/emberengine/engine/pkg/sysModule/gate/protocol_adapter/connx"
	"github.com/njtc406/emberengine/engine/pkg/utils/httpx"
	"github.com/njtc406/emberengine/engine/pkg/utils/httpx/router_center"
	"github.com/njtc406/emberengine/engine/pkg/utils/jwtx"
)

type WebSocketAdapter struct {
	server     *httpx.GinServer
	md         inf.IModule
	sessionMgr inf.ISessionManager
	jwtSecret  string
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

func (w *WebSocketAdapter) ListenAndServe(md inf.IModule, conf interface{}) error {
	w.md = md
	cfg, ok := conf.(*config.WSServerConf)
	if !ok {
		return fmt.Errorf("invalid websocket configuration")
	}
	w.jwtSecret = cfg.JWTSecret
	status := glbConfig.Release
	if service := md.GetService(); service != nil {
		if provider, ok := service.(interface{ GetNodeContext() inf.INodeContext }); ok {
			if nodeCtx := provider.GetNodeContext(); nodeCtx != nil {
				if c := nodeCtx.GetConfig(); c != nil {
					status = c.GetStatus()
				}
			}
		}
	}

	if err := w.server.Init(md.GetService().GetLogger(), status, cfg.HttpConf); err != nil {
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
		// 鉴权在中间件的时候就已经执行了,所以这里可以直接等同于连接成功,开始正常执行逻辑
		uid := gc.GetInt64("uid")
		if uid <= 0 {
			gc.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing or invalid uid"})
			return
		}

		c, err := upGrader.Upgrade(gc.Writer, gc.Request, nil)
		if err != nil {
			gc.String(http.StatusBadRequest, "upgrade failed: %v", err)
			return
		}

		conn := connx.NewWSConn(c)
		w.sessionMgr.Bind(uid, conn)
	})
}

func (w *WebSocketAdapter) Auth(c *gin.Context) {
	authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing or invalid Authorization header"})
		return
	}

	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	tokenString = strings.TrimSpace(tokenString)

	var claims *jwtx.EmberClaims
	var err error
	if w.jwtSecret != "" {
		claims, err = jwtx.ParseJwtTokenWithSecret(w.jwtSecret, tokenString)
	} else {
		claims, err = jwtx.ParseJwtToken(tokenString)
	}
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	c.Set("uid", claims.UserID)

	// 认证成功，继续执行下一个中间件或路由处理函数
	c.Next()
}

func (w *WebSocketAdapter) Shutdown(ctx context.Context) error {
	w.server.Stop()
	return nil
}

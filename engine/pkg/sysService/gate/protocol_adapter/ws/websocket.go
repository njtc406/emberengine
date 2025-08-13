// Package adapter_ws
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/8/14 0014 0:38
// 最后更新:  yr  2025/8/14 0014 0:38
package ws

import (
	"context"
	"fmt"
	"github.com/gorilla/websocket"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/sysService/gate/config"
	"github.com/njtc406/emberengine/engine/pkg/sysService/gate/protocol_adapter/connx"
	"net/http"
)

type WebSocketAdapter struct {
	server  *http.Server
	handler inf.IAdapterHandler
	svc     inf.IService
}

func NewWebSocketAdapter() *WebSocketAdapter {
	return &WebSocketAdapter{}
}

func (w *WebSocketAdapter) SetHandler(h inf.IAdapterHandler) {
	w.handler = h
}

func (w *WebSocketAdapter) ListenAndServe(svc inf.IService, conf interface{}) error {
	w.svc = svc
	cfg, ok := conf.(*config.WSServerConf)
	if !ok {
		return fmt.Errorf("invalid websocket configuration")
	}
	upGrader := websocket.Upgrader{}

	// TODO 这里再考虑下使用哪种server,应该可以使用gin来做这个,之后万一有其他需求,支持起来可能会更好一点
	mux := http.NewServeMux()

	mux.HandleFunc(cfg.Router, func(rw http.ResponseWriter, req *http.Request) {
		conn, err := upGrader.Upgrade(rw, req, nil)
		if err != nil {
			return
		}

		c := connx.NewWSConn(conn) // 实现 Conn 接口
		w.handler.OnConnect(c)

		// TODO 这里要修改,变为注入
		go func() {
			defer func() {
				w.handler.OnClose(c)
				c.Close()
			}()
			for {
				_, msg, err := conn.ReadMessage()
				if err != nil {
					return
				}
				w.handler.OnMessage(c, msg)
			}
		}()
	})

	// TODO 配置
	w.server = &http.Server{
		Addr:                         cfg.Addr,
		Handler:                      mux,
		DisableGeneralOptionsHandler: false,
		TLSConfig:                    cfg.TLS,
		ReadTimeout:                  0,
		ReadHeaderTimeout:            0,
		WriteTimeout:                 0,
		IdleTimeout:                  0,
		MaxHeaderBytes:               0,
	}

	if cfg.TLS != nil {
		return w.server.ListenAndServeTLS(cfg.CertFile, cfg.KeyFile)
	} else {
		return w.server.ListenAndServe()
	}
}

func (w *WebSocketAdapter) Shutdown(ctx context.Context) error {
	return w.server.Shutdown(ctx)
}

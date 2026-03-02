package network

import (
	"crypto/tls"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/log"

	"github.com/gorilla/websocket"
)

type WSServer struct {
	Addr            string
	Logger          *log.Logger
	MaxConnNum      int
	PendingWriteNum int
	MaxMsgLen       uint32
	HTTPTimeout     time.Duration
	CertFile        string
	KeyFile         string
	NewAgent        func(*WSConn) Agent
	ln              net.Listener
	handler         *WSHandler
	messageType     int
}

type WSHandler struct {
	logger          *log.Logger
	maxConnNum      int
	pendingWriteNum int
	maxMsgLen       uint32
	newAgent        func(*WSConn) Agent
	upgrader        websocket.Upgrader
	conns           WebsocketConnSet
	mutexConns      sync.Mutex
	wg              sync.WaitGroup
	messageType     int
}

func (handler *WSHandler) SetMessageType(messageType int) {
	handler.messageType = messageType
}

func (handler *WSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := handler.upgrader.Upgrade(w, r, nil)
	if err != nil {
		if handler.logger != nil {
			handler.logger.Errorf("upgrade fail, error: %s", err)
		}
		return
	}
	conn.SetReadLimit(int64(handler.maxMsgLen))
	if handler.messageType == 0 {
		handler.messageType = websocket.TextMessage
	}

	handler.wg.Add(1)
	defer handler.wg.Done()

	handler.mutexConns.Lock()
	if handler.conns == nil {
		handler.mutexConns.Unlock()
		conn.Close()
		return
	}
	if handler.maxConnNum > 0 && len(handler.conns) >= handler.maxConnNum {
		handler.mutexConns.Unlock()
		conn.Close()
		if handler.logger != nil {
			handler.logger.Warning("too many connections")
		}
		return
	}
	handler.conns[conn] = struct{}{}
	handler.mutexConns.Unlock()

	// TODO 验签

	wsConn := NewWSConn(conn, handler.pendingWriteNum, handler.maxMsgLen, handler.messageType, handler.logger)
	agent := handler.newAgent(wsConn)
	if agent == nil {
		wsConn.Close()
		handler.mutexConns.Lock()
		delete(handler.conns, conn)
		handler.mutexConns.Unlock()
		return
	}
	agent.Run()

	// cleanup
	wsConn.Close()
	handler.mutexConns.Lock()
	delete(handler.conns, conn)
	handler.mutexConns.Unlock()
	agent.OnClose()
}

func (server *WSServer) SetMessageType(messageType int) {
	server.messageType = messageType
	if server.handler != nil {
		server.handler.SetMessageType(messageType)
	}
}

func (server *WSServer) Start() {
	logger := server.Logger
	ln, err := net.Listen("tcp", server.Addr)
	if err != nil {
		if logger != nil {
			logger.Errorf("WSServer listen failed: %v", err)
		}
		return
	}

	if server.MaxConnNum <= 0 {
		server.MaxConnNum = 100
		if logger != nil {
			logger.Debugf("invalid MaxConnNum, reset: %d", server.MaxConnNum)
		}
	}
	if server.PendingWriteNum <= 0 {
		server.PendingWriteNum = 100
		if logger != nil {
			logger.Debugf("invalid PendingWriteNum, reset: %d", server.PendingWriteNum)
		}
	}
	if server.MaxMsgLen <= 0 {
		server.MaxMsgLen = 4096
		if logger != nil {
			logger.Debugf("invalid MaxMsgLen, reset: %d", server.MaxMsgLen)
		}
	}
	if server.HTTPTimeout <= 0 {
		server.HTTPTimeout = 60 * time.Second
		if logger != nil {
			logger.Debugf("invalid HTTPTimeout, reset: %d", server.HTTPTimeout)
		}
	}
	if server.NewAgent == nil {
		if logger != nil {
			logger.Error("WSServer NewAgent must not be nil")
		}
		_ = ln.Close()
		return
	}

	if server.CertFile != "" || server.KeyFile != "" {
		config := &tls.Config{}
		config.NextProtos = []string{"http/1.1"}

		var err error
		config.Certificates = make([]tls.Certificate, 1)
		config.Certificates[0], err = tls.LoadX509KeyPair(server.CertFile, server.KeyFile)
		if err != nil {
			if logger != nil {
				logger.Errorf("WSServer load cert failed: %v", err)
			}
			_ = ln.Close()
			return
		}

		ln = tls.NewListener(ln, config)
	}

	server.ln = ln
	server.handler = &WSHandler{
		logger:          logger,
		maxConnNum:      server.MaxConnNum,
		pendingWriteNum: server.PendingWriteNum,
		maxMsgLen:       server.MaxMsgLen,
		newAgent:        server.NewAgent,
		conns:           make(WebsocketConnSet),
		messageType:     server.messageType,
		upgrader: websocket.Upgrader{
			HandshakeTimeout: server.HTTPTimeout,
			CheckOrigin:      func(_ *http.Request) bool { return true },
		},
	}

	httpServer := &http.Server{
		Addr:           server.Addr,
		Handler:        server.handler,
		ReadTimeout:    server.HTTPTimeout,
		WriteTimeout:   server.HTTPTimeout,
		MaxHeaderBytes: 1024,
	}

	go httpServer.Serve(ln)
}

func (server *WSServer) Close() {
	server.ln.Close()

	server.handler.mutexConns.Lock()
	for conn := range server.handler.conns {
		conn.Close()
	}
	server.handler.conns = nil
	server.handler.mutexConns.Unlock()

	server.handler.wg.Wait()
}

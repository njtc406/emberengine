package network

import (
	"sync"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/log"

	"github.com/gorilla/websocket"
)

type WSClient struct {
	sync.Mutex
	Addr             string
	Logger           *log.Logger
	ConnNum          int
	ConnectInterval  time.Duration
	PendingWriteNum  int
	MaxMsgLen        uint32
	MessageType      int
	HandshakeTimeout time.Duration
	AutoReconnect    bool
	NewAgent         func(*WSConn) Agent
	dialer           websocket.Dialer
	cons             WebsocketConnSet
	wg               sync.WaitGroup
	closeFlag        bool
}

func (client *WSClient) Start() {
	if !client.init() {
		return
	}

	for i := 0; i < client.ConnNum; i++ {
		client.wg.Add(1)
		go client.connect()
	}
}

func (client *WSClient) init() bool {
	client.Lock()
	defer client.Unlock()
	logger := client.Logger

	if client.ConnNum <= 0 {
		client.ConnNum = 1
		if logger != nil {
			logger.Debugf("invalid ConnNum reset: %d", client.ConnNum)
		}
	}
	if client.ConnectInterval <= 0 {
		client.ConnectInterval = 3 * time.Second
		if logger != nil {
			logger.Debugf("invalid ConnectInterval reset: %d", client.ConnectInterval)
		}
	}
	if client.PendingWriteNum <= 0 {
		client.PendingWriteNum = 100
		if logger != nil {
			logger.Debugf("invalid PendingWriteNum reset: %d", client.PendingWriteNum)
		}
	}
	if client.MaxMsgLen <= 0 {
		client.MaxMsgLen = 4096
		if logger != nil {
			logger.Debugf("invalid MaxMsgLen reset: %d", client.MaxMsgLen)
		}
	}
	if client.HandshakeTimeout <= 0 {
		client.HandshakeTimeout = 10 * time.Second
		if logger != nil {
			logger.Debugf("invalid HandshakeTimeout reset: %d", client.HandshakeTimeout)
		}
	}
	if client.NewAgent == nil {
		if logger != nil {
			logger.Error("WSClient NewAgent must not be nil")
		}
		return false
	}
	if client.cons != nil {
		if logger != nil {
			logger.Error("WSClient client is running")
		}
		return false
	}

	if client.MessageType == 0 {
		client.MessageType = websocket.TextMessage
	}

	client.cons = make(WebsocketConnSet)
	client.closeFlag = false
	client.dialer = websocket.Dialer{
		HandshakeTimeout: client.HandshakeTimeout,
	}
	return true
}

func (client *WSClient) dial() *websocket.Conn {
	for {
		conn, _, err := client.dialer.Dial(client.Addr, nil)
		if err == nil || client.closeFlag {
			return conn
		}

		if client.Logger != nil {
			client.Logger.Debugf("connect fail, addr: %s, error: %v", client.Addr, err)
		}
		time.Sleep(client.ConnectInterval)
		continue
	}
}

func (client *WSClient) connect() {
	defer client.wg.Done()

reconnect:
	conn := client.dial()
	if conn == nil {
		return
	}
	conn.SetReadLimit(int64(client.MaxMsgLen))

	client.Lock()
	if client.closeFlag {
		client.Unlock()
		conn.Close()
		return
	}
	client.cons[conn] = struct{}{}
	client.Unlock()

	wsConn := NewWSConn(conn, client.PendingWriteNum, client.MaxMsgLen, client.MessageType, client.Logger)
	agent := client.NewAgent(wsConn)
	agent.Run()

	// cleanup
	wsConn.Close()
	client.Lock()
	delete(client.cons, conn)
	client.Unlock()
	agent.OnClose()

	if client.AutoReconnect {
		time.Sleep(client.ConnectInterval)
		goto reconnect
	}
}

func (client *WSClient) Close() {
	client.Lock()
	client.closeFlag = true
	for conn := range client.cons {
		conn.Close()
	}
	client.cons = nil
	client.Unlock()

	client.wg.Wait()
}

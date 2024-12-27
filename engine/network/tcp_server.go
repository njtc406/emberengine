package network

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/njtc406/emberengine/engine/utils/bytespool"
	"github.com/njtc406/emberengine/engine/utils/log"
)

const (
	Default_ReadDeadline    = time.Second * 30 //默认读超时30s
	Default_WriteDeadline   = time.Second * 30 //默认写超时30s
	Default_MaxConnNum      = 1000000          //默认最大连接数
	Default_PendingWriteNum = 100000           //单连接写消息Channel容量
	Default_LittleEndian    = false            //默认大小端
	Default_MinMsgLen       = 2                //最小消息长度2byte
	Default_LenMsgLen       = 2                //包头字段长度占用2byte
	Default_MaxMsgLen       = 65535            //最大消息长度
)

type TCPServer struct {
	Addr            string
	MaxConnNum      int
	PendingWriteNum int
	ReadDeadline    time.Duration
	WriteDeadline   time.Duration

	NewAgent   func(*TCPConn) Agent
	ln         net.Listener
	conns      ConnSet
	mutexConns sync.Mutex
	wgLn       sync.WaitGroup
	wgConns    sync.WaitGroup

	MsgParser
}

func (server *TCPServer) Start() error {
	err := server.init()
	if err != nil {
		return err
	}
	go server.run()

	return nil
}

func (server *TCPServer) init() error {
	ln, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("Listen tcp fail,error:%s", err.Error())
	}

	if server.MaxConnNum <= 0 {
		server.MaxConnNum = Default_MaxConnNum
		log.SysLogger.Infof("invalid MaxConnNum, reset to %d", server.MaxConnNum)
	}

	if server.PendingWriteNum <= 0 {
		server.PendingWriteNum = Default_PendingWriteNum
		log.SysLogger.Infof("invalid PendingWriteNum, reset to %d", server.PendingWriteNum)
	}

	if server.LenMsgLen <= 0 {
		server.LenMsgLen = Default_LenMsgLen
		log.SysLogger.Infof("invalid LenMsgLen, reset to %d", server.LenMsgLen)
	}

	if server.MaxMsgLen <= 0 {
		server.MaxMsgLen = Default_MaxMsgLen
		log.SysLogger.Infof("invalid MaxMsgLen, reset to %d", server.MaxMsgLen)
	}

	maxMsgLen := server.MsgParser.getMaxMsgLen(server.LenMsgLen)
	if server.MaxMsgLen > maxMsgLen {
		server.MaxMsgLen = maxMsgLen
		log.SysLogger.Infof("invalid MaxMsgLen, reset to %d", server.MaxMsgLen)
	}

	if server.MinMsgLen <= 0 {
		server.MinMsgLen = Default_MinMsgLen
		log.SysLogger.Infof("invalid MinMsgLen, reset to %d", server.MinMsgLen)
	}

	if server.WriteDeadline == 0 {
		server.WriteDeadline = Default_WriteDeadline
		log.SysLogger.Infof("invalid WriteDeadline, reset to %f", server.WriteDeadline.Seconds())
	}

	if server.ReadDeadline == 0 {
		server.ReadDeadline = Default_ReadDeadline
		log.SysLogger.Infof("invalid ReadDeadline, reset to %f", server.ReadDeadline.Seconds())
	}

	if server.NewAgent == nil {
		return errors.New("NewAgent must not be nil")
	}

	server.ln = ln
	server.conns = make(ConnSet)
	server.MsgParser.init()

	return nil
}

func (server *TCPServer) SetNetMempool(mempool bytespool.IBytesMempool) {
	server.IBytesMempool = mempool
}

func (server *TCPServer) GetNetMempool() bytespool.IBytesMempool {
	return server.IBytesMempool
}

func (server *TCPServer) run() {
	server.wgLn.Add(1)
	defer server.wgLn.Done()

	var tempDelay time.Duration
	for {
		conn, err := server.ln.Accept()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				if tempDelay == 0 {
					tempDelay = 5 * time.Millisecond
				} else {
					tempDelay *= 2
				}
				if max := 1 * time.Second; tempDelay > max {
					tempDelay = max
				}
				log.SysLogger.Infof("accept fail, %s, sleep %d ms", err.Error(), tempDelay/time.Millisecond)
				time.Sleep(tempDelay)
				continue
			}
			return
		}

		conn.(*net.TCPConn).SetNoDelay(true)
		tempDelay = 0

		server.mutexConns.Lock()
		if len(server.conns) >= server.MaxConnNum {
			server.mutexConns.Unlock()
			conn.Close()
			log.SysLogger.Warningf("too many connections")
			continue
		}

		server.conns[conn] = struct{}{}
		server.mutexConns.Unlock()
		server.wgConns.Add(1)

		tcpConn := newTCPConn(conn, server.PendingWriteNum, &server.MsgParser, server.WriteDeadline)
		agent := server.NewAgent(tcpConn)

		go func() {
			agent.Run()
			// cleanup
			tcpConn.Close()
			server.mutexConns.Lock()
			delete(server.conns, conn)
			server.mutexConns.Unlock()
			agent.OnClose()

			server.wgConns.Done()
		}()
	}
}

func (server *TCPServer) Close() {
	server.ln.Close()
	server.wgLn.Wait()

	server.mutexConns.Lock()
	for conn := range server.conns {
		conn.Close()
	}
	server.conns = nil
	server.mutexConns.Unlock()
	server.wgConns.Wait()
}

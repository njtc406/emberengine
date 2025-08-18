// Package conn
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/8/14 0014 0:43
// 最后更新:  yr  2025/8/14 0014 0:43
package connx

import (
	"context"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"net"
)

// WebSocket
type WSConn struct {
	conn *websocket.Conn
}

func NewWSConn(c *websocket.Conn) *WSConn {
	return &WSConn{conn: c}
}

func (c *WSConn) Send(data []byte) error { return c.conn.WriteMessage(websocket.BinaryMessage, data) }
func (c *WSConn) Close() error           { return c.conn.Close() }
func (c *WSConn) GetClientIp() string    { return c.conn.RemoteAddr().String() }

func (c *WSConn) ReadMessage() ([]byte, error) {
	_, data, err := c.conn.ReadMessage()
	return data, err
}

// TCP
type TCPConn struct {
	id   string
	conn net.Conn
	ctx  context.Context
}

func NewTCPConn(conn net.Conn) *TCPConn {
	return &TCPConn{id: uuid.NewString(), conn: conn, ctx: context.Background()}
}
func (c *TCPConn) Send(data []byte) error { _, err := c.conn.Write(data); return err }
func (c *TCPConn) Close() error           { return c.conn.Close() }
func (c *TCPConn) GetClientIp() string    { return c.conn.RemoteAddr().String() }

func (c *TCPConn) ReadMessage() ([]byte, error) {
	buf := make([]byte, 1024)
	n, err := c.conn.Read(buf)
	return buf[:n], err
}

// UDP
type UDPConn struct {
	conn *net.UDPConn
	addr *net.UDPAddr
}

func NewUDPConn(conn *net.UDPConn, addr *net.UDPAddr) *UDPConn {
	return &UDPConn{conn: conn, addr: addr}
}
func (c *UDPConn) Send(data []byte) error { _, err := c.conn.WriteToUDP(data, c.addr); return err }
func (c *UDPConn) Close() error           { return nil }
func (c *UDPConn) GetClientIp() string    { return c.addr.String() }
func (c *UDPConn) ReadMessage() ([]byte, error) {
	buf := make([]byte, 1024)
	n, err := c.conn.Read(buf)
	return buf[:n], err
}

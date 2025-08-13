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
	"net/http"
)

// WebSocket
type WSConn struct {
	id   string
	conn *websocket.Conn
	ctx  context.Context
}

func NewWSConn(c *websocket.Conn) *WSConn {
	return &WSConn{id: uuid.NewString(), conn: c, ctx: context.Background()}
}
func (c *WSConn) GetConnId() string              { return c.id }
func (c *WSConn) Send(data []byte) error         { return c.conn.WriteMessage(websocket.BinaryMessage, data) }
func (c *WSConn) Close() error                   { return c.conn.Close() }
func (c *WSConn) GetClientIp() string            { return c.conn.RemoteAddr().String() }
func (c *WSConn) Context() context.Context       { return c.ctx }
func (c *WSConn) SetContext(ctx context.Context) { c.ctx = ctx }

// HTTP
type HTTPConn struct {
	id     string
	writer http.ResponseWriter
	req    *http.Request
	ctx    context.Context
}

func NewHTTPConn(w http.ResponseWriter, r *http.Request) *HTTPConn {
	return &HTTPConn{id: uuid.NewString(), writer: w, req: r, ctx: context.Background()}
}
func (c *HTTPConn) ID() string                     { return c.id }
func (c *HTTPConn) Send(data []byte) error         { _, err := c.writer.Write(data); return err }
func (c *HTTPConn) Close() error                   { return nil }
func (c *HTTPConn) RemoteAddr() string             { return c.req.RemoteAddr }
func (c *HTTPConn) Context() context.Context       { return c.ctx }
func (c *HTTPConn) SetContext(ctx context.Context) { c.ctx = ctx }

// TCP
type TCPConn struct {
	id   string
	conn net.Conn
	ctx  context.Context
}

func NewTCPConn(conn net.Conn) *TCPConn {
	return &TCPConn{id: uuid.NewString(), conn: conn, ctx: context.Background()}
}
func (c *TCPConn) ID() string                     { return c.id }
func (c *TCPConn) Send(data []byte) error         { _, err := c.conn.Write(data); return err }
func (c *TCPConn) Close() error                   { return c.conn.Close() }
func (c *TCPConn) RemoteAddr() string             { return c.conn.RemoteAddr().String() }
func (c *TCPConn) Context() context.Context       { return c.ctx }
func (c *TCPConn) SetContext(ctx context.Context) { c.ctx = ctx }

// UDP
type UDPConn struct {
	id   string
	conn *net.UDPConn
	addr *net.UDPAddr
	ctx  context.Context
}

func NewUDPConn(conn *net.UDPConn, addr *net.UDPAddr) *UDPConn {
	return &UDPConn{id: uuid.NewString(), conn: conn, addr: addr, ctx: context.Background()}
}
func (c *UDPConn) ID() string                     { return c.id }
func (c *UDPConn) Send(data []byte) error         { _, err := c.conn.WriteToUDP(data, c.addr); return err }
func (c *UDPConn) Close() error                   { return nil }
func (c *UDPConn) RemoteAddr() string             { return c.addr.String() }
func (c *UDPConn) Context() context.Context       { return c.ctx }
func (c *UDPConn) SetContext(ctx context.Context) { c.ctx = ctx }

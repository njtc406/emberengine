// Package wsmodule
// @Title  请填写文件名称（需要改）
// @Description  请填写文件描述（需要改）
// @Author  yr  2025/1/21 上午10:33
// @Update  yr  2024/1/21 上午10:33
package wsmodule

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/network"
)

type Client struct {
	mgr    *ClientMgr
	status atomic.Int32
	closed chan struct{}
	wg     sync.WaitGroup

	roleID string
	conn   *network.WSConn // 连接
}

func newClient(mgr *ClientMgr, conn *network.WSConn) *Client {
	return &Client{
		mgr:    mgr,
		conn:   conn,
		closed: make(chan struct{}),
	}
}

func (c *Client) Run() {
	for {
		select {
		case <-c.closed:
			return
		default:
			if err := c.ReadMsg(); err != nil {
				log.SysLogger.Errorf("Client read msg error: %s", err)
			}
		}
	}
}

// Close 关闭连接(所有外部想要断开这个客户端,都是用这个接口)
func (c *Client) Close() {
	c.status.Store(1)
	close(c.closed)

	// 先从客户端管理器移除
	if c.roleID != "" {
		c.mgr.delClient(c.roleID)
	}
}

func (c *Client) OnClose() {
	// 在run返回之后,底层会自动关闭连接,这里只需要置空变量
	c.conn = nil
}

func (c *Client) BindRole(roleID string) {
	c.roleID = roleID
}

func (c *Client) GetRoleID() string {
	return c.roleID
}

func (c *Client) SendMsg(msg interface{}) error {
	if c.roleID == "" {
		return errors.New("roleID invalid")
	}
	data, err := c.mgr.Marshal(c.roleID, msg)
	if err != nil {
		return err
	}
	return c.conn.WriteMsg(data)
}

func (c *Client) ReadMsg() error {
	if c.roleID == "" {
		return errors.New("roleID invalid")
	}
	msg, err := c.conn.ReadMsg()
	if err != nil {
		log.SysLogger.Errorf("c.conn.ReadMsg err %v", err)
		return err
	}

	// 消息解析
	info, err := c.mgr.Unmarshal(c.roleID, msg)
	if err != nil {
		log.SysLogger.Errorf("Client receive msg error: %s", err)
		return err
	}

	return c.mgr.MsgRoute(c.roleID, info)
}

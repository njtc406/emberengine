// Package wsmodule
// @Title  请填写文件名称（需要改）
// @Description  请填写文件描述（需要改）
// @Author  yr  2025/1/21 上午10:33
// @Update  yr  2024/1/21 上午10:33
package wsmodule

import (
	"context"
	"github.com/google/uuid"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/utils/emberctx"
	"sync"
	"sync/atomic"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/event"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/network"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
)

const (
	upGrade = iota
	running
	closed
)

type Client struct {
	mgr    *ClientMgr
	status atomic.Int32
	wg     sync.WaitGroup

	msgCnt  uint64
	timerId uint64

	sessionId int64
	roleId    string
	conn      *network.WSConn // 连接
}

func newClient(mgr *ClientMgr, conn *network.WSConn) *Client {
	c := &Client{
		mgr:       mgr,
		conn:      conn,
		sessionId: mgr.GenSessionId(),
	}
	c.status.Store(upGrade)
	return c
}

func (c *Client) Run() {
	c.wg.Add(1)
	go c.listen()
	c.mgr.NotifyEvent(&event.Event{
		Type: event.SysEventWebSocket,
		Data: &WSPack{
			Type:      WPTConnected,
			SessionId: c.sessionId,
			Data:      c,
		},
	})
	// 启动一个定时器,在10秒后检查是否绑定了roleId,如果没有绑定,则通知客户端断开连接
	// TODO (这个具体时间之后根据需求调整)
	c.timerId, _ = c.mgr.AfterFuncWithStorage(time.Second*10, "gate_client_check_auth", c.checkAuth)
	c.wg.Wait()
}

func (c *Client) listen() {
	defer c.wg.Done()
	for {
		msg, err := c.conn.ReadMsg()
		if err != nil {
			log.SysLogger.Errorf("c.conn.ReadMsg err %v", err)
			return
		}

		// 消息解析
		info, err := c.mgr.Unmarshal(msg)
		if err != nil {
			log.SysLogger.Errorf("Client receive msg error: %s", err)
			c.mgr.NotifyEvent(&event.Event{
				Type: event.SysEventWebSocket,
				Data: &WSPack{
					Type:      WPTUnknownPack,
					ClientId:  c.roleId,
					SessionId: c.sessionId,
					Data:      msg,
				},
			})
			continue
		}

		if c.msgCnt > 0 && !c.IsRunning() {
			// 在接收了auth消息之后,如果没有绑定角色,则不处理后续消息
			continue
		}

		c.msgCnt++

		ctx := context.Background()
		emberctx.AddHeader(ctx, def.DefaultTraceIdKey, uuid.NewString())
		emberctx.AddHeader(ctx, def.DefaultDispatcherKey, c.roleId)

		c.mgr.NotifyEvent(&event.Event{
			Type: event.SysEventWebSocket,
			Data: &WSPack{
				Type:      WPTPack,
				ClientId:  c.roleId,
				SessionId: c.sessionId,
				Data:      info,
				Ctx:       ctx,
			},
		})
	}
}

// Close 关闭连接(所有外部想要断开这个客户端,都是用这个接口)
func (c *Client) Close() {
	if c.status.Load() == closed {
		return
	}
	c.status.Store(closed)
	if c.mgr.Cancel(atomic.LoadUint64(&c.timerId)) {
		atomic.StoreUint64(&c.timerId, 0)
	}
	if c.conn == nil {
		return
	}
	c.conn.Close()
}

func (c *Client) OnClose() {
	// 这个事件放在这是为了保证只调用一次
	c.mgr.NotifyEvent(&event.Event{
		Type: event.SysEventWebSocket,
		Data: &WSPack{
			Type:      WPTDisConnected,
			SessionId: c.sessionId,
			ClientId:  c.roleId,
		},
	})

	// 在run返回之后,底层会自动关闭连接,这里只需要置空变量
	c.conn = nil
	c.roleId = ""
	c.sessionId = 0
}

func (c *Client) BindRole(roleId string) {
	// 移除timer
	if c.mgr.Cancel(atomic.LoadUint64(&c.timerId)) {
		atomic.StoreUint64(&c.timerId, 0)
	}

	c.roleId = roleId
	// 绑定玩家数据才算运行中
	c.status.Store(running)
	c.mgr.NotifyEvent(&event.Event{
		Type: event.SysEventWebSocket,
		Data: &WSPack{
			Type:      WPTReady,
			SessionId: c.sessionId,
			ClientId:  roleId,
			Data:      c,
		},
	})
}

func (c *Client) IsRunning() bool {
	return c.status.Load() == running
}

func (c *Client) isClosed() bool {
	return c.status.Load() == closed
}

func (c *Client) SendMsg(id int32, msg interface{}) error {
	if c.isClosed() {
		return nil
	}
	data, err := c.mgr.Marshal(id, msg)
	if err != nil {
		return err
	}
	return c.conn.WriteMsg(data)
}

func (c *Client) ReadMsg() error {
	defer func() {
		if r := recover(); r != nil {
			log.SysLogger.Errorf("Client read msg error: %s", r)
		}
	}()
	msg, err := c.conn.ReadMsg()
	if err != nil {
		log.SysLogger.Errorf("c.conn.ReadMsg err %v", err)
		return err
	}

	// 消息解析
	info, err := c.mgr.Unmarshal(msg)
	if err != nil {
		log.SysLogger.Errorf("Client receive msg error: %s", err)
		c.mgr.NotifyEvent(&event.Event{
			Type: event.SysEventWebSocket,
			Data: &WSPack{
				Type:      WPTUnknownPack,
				ClientId:  c.roleId,
				SessionId: c.sessionId,
				Data:      msg,
			},
		})
		return err
	}

	if c.msgCnt > 0 && !c.IsRunning() {
		// 在接收了auth消息之后,如果没有绑定角色,则不处理后续消息
		return nil
	}

	c.msgCnt++

	c.mgr.NotifyEvent(&event.Event{
		Type: event.SysEventWebSocket,
		Data: &WSPack{
			Type:      WPTPack,
			ClientId:  c.roleId,
			SessionId: c.sessionId,
			Data:      info,
		},
	})

	return nil
}

func (c *Client) checkAuth(_ *timingwheel.Timer, _ ...interface{}) {
	if c.roleId == "" {
		atomic.StoreUint64(&c.timerId, 0)
		c.Close()
	}
}

func (c *Client) GetClientIp() string {
	return c.conn.RemoteAddr().String()
}

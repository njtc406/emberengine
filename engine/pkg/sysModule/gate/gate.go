// Package gate
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/8/14 0014 0:06
// 最后更新:  yr  2025/8/14 0014 0:06
package gate

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/core"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/sysModule/gate/config"
	"github.com/njtc406/emberengine/engine/pkg/utils/xcontext"
)

// RestartPolicy 配置 Gate 监听失败后的重启策略。
type RestartPolicy struct {
	Enable         bool          `binding:""`
	MaxRestart     int           `binding:"min=0"`
	InitialBackoff time.Duration `binding:"min=0"`
	MaxBackoff     time.Duration `binding:"min=0"`
}

// DefaultRestartPolicy 提供合理的默认值。
func DefaultRestartPolicy() RestartPolicy {
	return RestartPolicy{
		Enable:         true,
		MaxRestart:     5,
		InitialBackoff: 500 * time.Millisecond,
		MaxBackoff:     10 * time.Second,
	}
}

type Gate struct {
	core.Module

	adapter inf.IProtocolAdapter

	// supervisor 状态
	ctx          context.Context
	cancel       context.CancelFunc
	serveDone    chan struct{}
	restartCount atomic.Int64
	serving      atomic.Bool
	lastErr      atomic.Value // stores error

	restartPolicy RestartPolicy
}

func NewGate() *Gate {
	return &Gate{
		restartPolicy: DefaultRestartPolicy(),
	}
}

func (g *Gate) OnInit() error {
	return nil
}

// SetRestartPolicy 设置重启策略（应在 Start 前调用）。
func (g *Gate) SetRestartPolicy(p RestartPolicy) {
	g.restartPolicy = p
}

func (g *Gate) Start(conf *config.GateService) error {
	if g.adapter == nil {
		return nil
	}
	if conf == nil {
		return fmt.Errorf("gate service conf error")
	}

	// 从配置加载 RestartPolicy
	if conf.RestartPolicy != nil {
		g.restartPolicy = RestartPolicy{
			Enable:         conf.RestartPolicy.Enable,
			MaxRestart:     conf.RestartPolicy.MaxRestart,
			InitialBackoff: conf.RestartPolicy.InitialBackoff,
			MaxBackoff:     conf.RestartPolicy.MaxBackoff,
		}
	}

	var sConf interface{}
	switch conf.Type {
	case "ws":
		sConf = conf.WSServerConf
	case "http":
		sConf = conf.HttpServerConf
	case "tcp":
		sConf = conf.TcpServerConf
	case "udp":
		sConf = conf.UdpServerConf
	default:
		return nil
	}

	g.ctx, g.cancel = context.WithCancel(context.Background())
	g.serveDone = make(chan struct{})

	go g.superviseServe(sConf)
	return nil
}

// superviseServe 循环启动 ListenAndServe，按策略重启或放弃。
func (g *Gate) superviseServe(sConf interface{}) {
	defer close(g.serveDone)

	backoff := g.restartPolicy.InitialBackoff
	if backoff <= 0 {
		backoff = 500 * time.Millisecond
	}
	maxBackoff := g.restartPolicy.MaxBackoff
	if maxBackoff <= 0 {
		maxBackoff = 10 * time.Second
	}

	for {
		g.serving.Store(true)
		err := g.listenAndServeSafe(sConf)
		g.serving.Store(false)

		// 正常关闭（context 已取消）不重启
		if g.ctx.Err() != nil {
			return
		}

		// 无错误（正常 shutdown）不重启
		if err == nil {
			return
		}

		// 正常关闭导致的错误不重启
		if isNormalShutdown(err) {
			return
		}

		g.lastErr.Store(err)

		// 错误分类
		if isPermanentError(err) {
			if l := g.GetLogger(); l != nil {
				l.Errorf("gate: permanent listen error, not restarting: %v", err)
			}
			return
		}

		// 检查是否超过最大重启次数
		if g.restartPolicy.Enable && g.restartPolicy.MaxRestart > 0 {
			count := g.restartCount.Add(1)
			if count > int64(g.restartPolicy.MaxRestart) {
				if l := g.GetLogger(); l != nil {
					l.Errorf("gate: max restart count (%d) exceeded, giving up", g.restartPolicy.MaxRestart)
				}
				return
			}
		} else if !g.restartPolicy.Enable {
			// 未启用重启策略，失败即停
			if l := g.GetLogger(); l != nil {
				l.Warnf("gate: listen error (restart disabled): %v", err)
			}
			return
		}

		if l := g.GetLogger(); l != nil {
			l.Warnf("gate: listen error, restarting in %v (attempt %d/%d): %v",
				backoff, g.restartCount.Load(), g.restartPolicy.MaxRestart, err)
		}

		// 退避等待
		select {
		case <-time.After(backoff):
		case <-g.ctx.Done():
			return
		}

		// 指数退避
		backoff = backoff * 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (g *Gate) listenAndServeSafe(sConf interface{}) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("gate listen panic: %v", r)
		}
	}()
	return g.adapter.ListenAndServe(g, sConf)
}

// isPermanentError 判断是否为不可恢复错误（端口占用、配置错误等）。
func isPermanentError(err error) bool {
	// 端口已被占用
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if opErr.Op == "listen" {
			return true
		}
	}
	// 地址格式错误
	var addrErr *net.AddrError
	if errors.As(err, &addrErr) {
		return true
	}
	return false
}

// isNormalShutdown 判断是否为正常关闭导致的错误（不应重启）。
func isNormalShutdown(err error) bool {
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	// 兼容 Go 低版本中 "use of closed network connection" 错误
	if err != nil && strings.Contains(err.Error(), "use of closed network connection") {
		return true
	}
	return false
}

func (g *Gate) OnRelease() {
	if g.cancel != nil {
		g.cancel()
	}
	if g.adapter != nil {
		if err := g.adapter.Shutdown(xcontext.New(nil)); err != nil {
			if l := g.GetLogger(); l != nil {
				l.Errorf("shutdown error: %v", err)
			}
		}
	}
	// 等待 supervisor 退出
	if g.serveDone != nil {
		<-g.serveDone
	}
}

// IsServing 返回当前是否正在监听。
func (g *Gate) IsServing() bool {
	return g.serving.Load()
}

// LastServeError 返回最近一次监听失败的错误。
func (g *Gate) LastServeError() error {
	if v := g.lastErr.Load(); v != nil {
		return v.(error)
	}
	return nil
}

// RestartCount 返回已重启次数。
func (g *Gate) RestartCount() int64 {
	return g.restartCount.Load()
}

func (g *Gate) SetProtocolAdapter(adapter inf.IProtocolAdapter) {
	g.adapter = adapter
}

func (g *Gate) GetProtocolAdapter() inf.IProtocolAdapter {
	return g.adapter
}

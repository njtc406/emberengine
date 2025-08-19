// Package gate
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/8/14 0014 0:06
// 最后更新:  yr  2025/8/14 0014 0:06
package gate

import (
	"fmt"
	"github.com/njtc406/emberengine/engine/pkg/core"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/sysModule/gate/config"
	"github.com/njtc406/emberengine/engine/pkg/utils/xcontext"
)

type Gate struct {
	core.Module

	adapter inf.IProtocolAdapter
}

func NewGate() *Gate {
	return &Gate{}
}

func (g *Gate) OnInit() error {
	return nil
}

func (g *Gate) Start(conf *config.GateService) error {
	if g.adapter != nil {
		if conf == nil {
			return fmt.Errorf("gate service conf error")
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
		go func() {
			if err := g.adapter.ListenAndServe(g, sConf); err != nil {
				g.GetLogger().Warnf("listen and serve error: %v", err)
			}
		}()
	}
	return nil
}

func (g *Gate) OnRelease() {
	if g.adapter != nil {
		if err := g.adapter.Shutdown(xcontext.New(nil)); err != nil {
			g.GetLogger().Errorf("shutdown error: %v", err)
		}
	}
}

func (g *Gate) SetProtocolAdapter(adapter inf.IProtocolAdapter) {
	g.adapter = adapter
}

func (g *Gate) GetProtocolAdapter() inf.IProtocolAdapter {
	return g.adapter
}

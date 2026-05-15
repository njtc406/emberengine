// Package healthservice 提供 /health、/ready、/metrics 运维端点。
//
// 使用轻量 net/http（不依赖 gin），保证运维端点在极端情况下仍可响应。
// 服务通过 RegisterHealthService() 注册到框架，由 Node 统一管理生命周期。
package healthservice

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	systemConfig "github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/core"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/services"
	"github.com/njtc406/emberengine/engine/pkg/sysService/healthservice/config"
)

// RegisterHealthService 注册 HealthService 到框架。
func RegisterHealthService() {
	services.SetService("HealthService", func() inf.IService { return &HealthService{} })
	systemConfig.RegisterServiceConf(&systemConfig.ServiceConfig{
		ServiceName:   "HealthService",
		ConfName:      "health",
		ConfType:      "yaml",
		ConfPath:      "",
		CfgCreator:    func() interface{} { return &config.HealthConf{} },
		DefaultSetFun: config.SetHealthConfDefault,
		OnChangeFun:   func() {},
	})
}

// HealthService 提供运维 HTTP 端点。
type HealthService struct {
	core.Service

	server *http.Server
	once   sync.Once // Stop 幂等
}

func (hs *HealthService) getConf() *config.HealthConf {
	return hs.GetServiceCfg().(*config.HealthConf)
}

func (hs *HealthService) OnInit() error {
	conf := hs.getConf()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", hs.handleHealth)
	mux.HandleFunc("/ready", hs.handleReady)
	mux.HandleFunc("/metrics", hs.handleMetrics)

	hs.server = &http.Server{
		Addr:              conf.Addr,
		Handler:           mux,
		ReadHeaderTimeout: conf.ReadHeaderTimeout,
		IdleTimeout:       conf.IdleTimeout,
	}
	return nil
}

func (hs *HealthService) OnStart() error {
	go func() {
		if err := hs.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			hs.WithField("addr", hs.getConf().Addr).Errorf("health server ListenAndServe: %v", err)
		}
	}()
	hs.WithField("addr", hs.getConf().Addr).Info("health service started")
	return nil
}

func (hs *HealthService) OnRelease() {
	hs.once.Do(func() {
		if hs.server == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := hs.server.Shutdown(ctx); err != nil {
			hs.Errorf("health server shutdown: %v", err)
		}
	})
}

// --- HTTP Handlers ---

// handleHealth 存活检查：进程可响应即 200。
func (hs *HealthService) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "ok")
}

// handleReady 就绪检查：Node 已启动且未停止时 200。
func (hs *HealthService) handleReady(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	ctx := hs.GetNodeContext()
	if ctx == nil || !ctx.IsReady() {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, "not ready")
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "ready")
}

// handleMetrics Prometheus 指标端点。
func (hs *HealthService) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	ctx := hs.GetNodeContext()
	if ctx == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	text := ctx.GetRuntimeMetricsText()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, text)
}

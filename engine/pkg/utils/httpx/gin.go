// Package httpx
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/8/15 0015 0:10
// 最后更新:  yr  2025/8/15 0015 0:10
package httpx

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/httpx/router_center"
)

// Conf 配置信息
type Conf struct {
	// 服务监听端口
	Addr string
	// 服务读取头部超时时间
	ReadHeaderTimeout time.Duration
	// 服务空闲超时时间
	IdleTimeout time.Duration
	// 缓存目录(基于设置的系统缓存目录)
	CachePath string
	// 静态资源根目录
	ResourceRootPath string
	// html目录
	HttpDir string
	// 静态资源目录
	StaticDir string
	// basic auth认证用户名
	Account map[string]string
	// 证书文件
	CAFile *CAFile
	// 安全响应头配置(nil 表示使用默认配置)
	SecurityHeaders *SecurityHeadersConf
}

func (c *Conf) GetHttpDir() string {
	if 0 == len(c.HttpDir) {
		return ""
	}
	return path.Join(c.ResourceRootPath, c.HttpDir)
}

func (c *Conf) GetStaticDir() string {
	return path.Join(c.ResourceRootPath, c.StaticDir)
}

type CAFile struct {
	CertFile string
	KeyFile  string
}

type GinServer struct {
	handler *gin.Engine
	server  *http.Server
	router  *router_center.GroupHandlerPool
	conf    *Conf
	logger  log.ILoggerX

	middleware     []gin.HandlerFunc
	beforeServHook []func()
	initHook       []func()
	runHook        []func()
	stopHook       []func()

	wg sync.WaitGroup
}

func NewGinServer() *GinServer {
	return &GinServer{
		handler: gin.New(),
	}
}

func (gs *GinServer) Init(logger log.ILoggerX, systemMod string, conf *Conf) error {
	gs.conf = conf
	gs.logger = logger
	// 运行模式
	gin.SetMode(systemMod)
	// 设置中间件
	gs.handler.Use(
		gzip.Gzip(gzip.DefaultCompression),
		gs.customLoggerMiddleware(),
		gs.securityHeadersMiddleware(),
		gin.Recovery(),
	)
	// 自定义中间件
	for _, middleware := range gs.middleware {
		gs.handler.Use(middleware)
	}
	// 载入路由
	gs.router.RouteSet(gs.handler)
	gs.handler.ForwardedByClientIP = true
	// 设置静态资源目录
	htmlGlob := gs.conf.GetHttpDir()
	if 0 != len(htmlGlob) {
		gs.handler.LoadHTMLGlob(htmlGlob + "/*")
	}
	// 初始化服务
	gs.server = &http.Server{
		Addr:              gs.conf.Addr, // 服务监听端口
		Handler:           gs.handler,
		ReadHeaderTimeout: gs.conf.ReadHeaderTimeout,
		IdleTimeout:       gs.conf.IdleTimeout,
	}

	wg := new(sync.WaitGroup)
	for _, hook := range gs.initHook {
		wg.Add(1)
		// 保证所有的hook都执行完毕
		go func(h func()) {
			defer wg.Done()
			h()
		}(hook)
	}
	wg.Wait()
	return nil
}

func (gs *GinServer) logFormatter(p gin.LogFormatterParams) string {
	return fmt.Sprintf("[%s] %s %s %s %d %s \"%s\" %s\n",
		p.ClientIP,
		p.Method,
		p.Path,
		p.Request.Proto,
		p.StatusCode,
		p.Latency,
		p.Request.UserAgent(),
		p.ErrorMessage,
	)
}

func (gs *GinServer) Start() {
	gs.wg.Add(1)
	gs.run()
}

func (gs *GinServer) run() {
	defer gs.wg.Done()
	wg := new(sync.WaitGroup)
	for _, hook := range gs.runHook {
		wg.Add(1)
		// 保证所有的hook都执行完毕
		go func(h func()) {
			defer wg.Done()
			h()
		}(hook)
	}
	wg.Wait()

	if gs.server == nil {
		gs.logger.Errorf("server is nil trace:%s", debug.Stack())
	}

	gs.logger.Infof("listen %s", gs.server.Addr)
	if gs.conf.CAFile != nil {
		if err := gs.server.ListenAndServeTLS(gs.conf.CAFile.CertFile, gs.conf.CAFile.KeyFile); err != nil {
			gs.logger.Error(err)
		}
	} else {
		if err := gs.server.ListenAndServe(); err != nil {
			gs.logger.Warn(err)
		}
	}
}

func (gs *GinServer) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, hook := range gs.stopHook {
		hook()
	}

	if err := gs.server.Shutdown(ctx); err != nil {
		gs.logger.Warn(err)
	}
	gs.wg.Wait()
}

func (gs *GinServer) WithBeforeServHook(hooks ...func()) *GinServer {
	gs.beforeServHook = append(gs.beforeServHook, hooks...)
	return gs
}

func (gs *GinServer) WithInitHook(hooks ...func()) *GinServer {
	gs.initHook = append(gs.initHook, hooks...)
	return gs
}

func (gs *GinServer) WithRunHook(hooks ...func()) *GinServer {
	gs.runHook = append(gs.runHook, hooks...)
	return gs
}

func (gs *GinServer) WithStopHook(hooks ...func()) *GinServer {
	gs.stopHook = append(gs.stopHook, hooks...)
	return gs
}

func (gs *GinServer) WithMiddleware(middleware ...gin.HandlerFunc) *GinServer {
	gs.middleware = append(gs.middleware, middleware...)
	return gs
}

func (gs *GinServer) SetRouter(router *router_center.GroupHandlerPool) *GinServer {
	gs.router = router
	return gs
}

func (gs *GinServer) securityHeadersMiddleware() gin.HandlerFunc {
	conf := DefaultSecurityHeadersConf()
	if gs.conf != nil && gs.conf.SecurityHeaders != nil {
		conf = *gs.conf.SecurityHeaders
	}
	return SecurityHeaders(conf)
}

func (gs *GinServer) customLoggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		p := c.Request.URL.Path
		rawQuery := sanitizeQuery(c.Request.URL.RawQuery)
		c.Next()
		// 构建日志消息
		entry := gs.logger.WithFields(log.Fields{
			"status":     c.Writer.Status(),
			"method":     c.Request.Method,
			"path":       p,
			"query":      rawQuery,
			"ip":         c.ClientIP(),
			"latency":    time.Since(start),
			"user_agent": c.Request.UserAgent(),
		})
		// 添加错误信息（如果有）
		if len(c.Errors) > 0 {
			entry = entry.WithField("errors", c.Errors.String())
		}
		// 根据状态码级别决定日志级别
		status := c.Writer.Status()
		switch {
		case status >= 500:
			entry.Error("Request completed with server error")
		case status >= 400:
			entry.Warn("Request completed with client error")
		default:
			entry.Info("Request completed successfully")
		}
	}
}

// sensitiveQueryKeys 包含需要在日志中脱敏的查询参数名称。
var sensitiveQueryKeys = map[string]struct{}{
	"token":        {},
	"secret":       {},
	"password":     {},
	"passwd":       {},
	"access_token": {},
	"api_key":      {},
	"apikey":       {},
}

// sanitizeQuery 对 URL 查询字符串中的敏感参数进行脱敏处理。
func sanitizeQuery(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "[parse_error]"
	}
	redacted := false
	for key := range values {
		if _, ok := sensitiveQueryKeys[strings.ToLower(key)]; ok {
			values.Set(key, "[REDACTED]")
			redacted = true
		}
	}
	if !redacted {
		return rawQuery // 无敏感参数，原样返回避免重编码
	}
	return values.Encode()
}

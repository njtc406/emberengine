// Package httpmodule
// 模块名: http服务
// 功能描述: 这是一个公用的http服务器
// 作者:  yr  2024/1/4 0004 23:41
// 最后更新:  yr  2024/1/4 0004 23:41
package httpmodule

import (
	"context"
	"fmt"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"io"
	"net/http"
	"path"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/njtc406/emberengine/engine/pkg/core"
	"github.com/njtc406/emberengine/engine/pkg/sysModule/httpmodule/auth"
	"github.com/njtc406/emberengine/engine/pkg/sysModule/httpmodule/router_center"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
)

type HttpModule struct {
	core.Module
	logger     log.ILogger
	systemMod  string
	wg         *sync.WaitGroup
	running    uint32
	handler    *gin.Engine
	server     *http.Server
	router     *router_center.GroupHandlerPool
	beforeServ []func()
	conf       *Conf
	initHook   []func()
	runHook    []func()
	stopHook   []func()
	// 这里还需要一些事件池之类的东西，用来存储和触发特定的一些自定义钩子函数
	// 还需要一个服务通讯接口(这个接口提供几个固定方法,可能其中一个方法就是查询可以提供给其他服务调用的api之类的)
}

type CAFile struct {
	CertFile string
	KeyFile  string
}

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
	// 是否开启basic auth认证
	Auth bool
	// basic auth认证用户名
	Account map[string]string
	// 证书文件
	CAFile *CAFile
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

func (hs *HttpModule) OnInit() error {
	// 替换验证器(这个东西之后再看用哪个版本)
	//*(binding.Validator.Engine().(*validator.Validate)) = *validate.Validator
	// 默认日志输出
	gin.DefaultWriter = io.MultiWriter(hs.logger.WriterLevel(log.InfoLevel))       // 设置默认日志输出为info级别
	gin.DefaultErrorWriter = io.MultiWriter(hs.logger.WriterLevel(log.ErrorLevel)) // 设置默认错误日志输出为error级别
	// 运行模式
	gin.SetMode(hs.systemMod)
	// 默认中间件
	hs.handler.Use(
		gzip.Gzip(gzip.DefaultCompression),
		gin.LoggerWithFormatter(hs.logFormatter), // 这个设置的是默认日志的输出格式
		gin.Recovery(),
	)
	// 载入路由
	hs.router.RouteSet(hs.handler)
	if hs.conf.Auth {
		// 目前只支持basic auth
		hs.handler.Use(auth.BasicAuth(hs.conf.Account))
	}
	hs.handler.ForwardedByClientIP = true
	// 设置静态资源目录
	htmlGlob := hs.conf.GetHttpDir()
	if 0 != len(htmlGlob) {
		hs.handler.LoadHTMLGlob(htmlGlob + "/*")
	}
	// 初始化服务
	hs.server = &http.Server{
		Addr:              hs.conf.Addr, // 服务监听端口
		Handler:           hs.handler,
		ReadHeaderTimeout: hs.conf.ReadHeaderTimeout,
		IdleTimeout:       hs.conf.IdleTimeout,
	}

	wg := new(sync.WaitGroup)
	for _, hook := range hs.initHook {
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

func (hs *HttpModule) logFormatter(p gin.LogFormatterParams) string {
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

func (hs *HttpModule) OnStart() error {
	if !atomic.CompareAndSwapUint32(&hs.running, 0, 1) {
		return def.ErrServiceIsRunning
	}
	hs.wg.Add(1)
	go hs.run()

	return nil
}

func (hs *HttpModule) run() {
	defer hs.wg.Done()
	wg := new(sync.WaitGroup)
	for _, hook := range hs.runHook {
		wg.Add(1)
		// 保证所有的hook都执行完毕
		go func(h func()) {
			defer wg.Done()
			h()
		}(hook)
	}
	wg.Wait()

	if hs.server == nil {
		hs.logger.Errorf("server is nil trace:%s", debug.Stack())
	}

	hs.logger.Infof("listen %s", hs.server.Addr)
	if hs.conf.CAFile != nil {
		if err := hs.server.ListenAndServeTLS(hs.conf.CAFile.CertFile, hs.conf.CAFile.KeyFile); err != nil {
			hs.logger.Error(err)
		}
	} else {
		if err := hs.server.ListenAndServe(); err != nil {
			hs.logger.Error(err)
		}
	}
}

func (hs *HttpModule) OnRelease() {
	defer atomic.StoreUint32(&hs.running, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, hook := range hs.stopHook {
		hook()
	}

	if err := hs.server.Shutdown(ctx); err != nil {
		hs.logger.Warn(err)
	}
	hs.wg.Wait()
}

func (hs *HttpModule) WithInitHook(hook func()) *HttpModule {
	hs.initHook = append(hs.initHook, hook)
	return hs
}

func (hs *HttpModule) WithRunHook(hook func()) *HttpModule {
	hs.runHook = append(hs.runHook, hook)
	return hs
}

func (hs *HttpModule) WithStopHook(hook func()) *HttpModule {
	hs.stopHook = append(hs.stopHook, hook)
	return hs
}

func (hs *HttpModule) SetRouter(router *router_center.GroupHandlerPool) *HttpModule {
	hs.router = router
	return hs
}

// NewHttpModule 创建新的HTTP服务器
func NewHttpModule(conf *Conf, logger log.ILogger, systemMod string) *HttpModule {
	return &HttpModule{
		handler:   gin.New(),
		wg:        new(sync.WaitGroup),
		conf:      conf,
		logger:    logger,
		systemMod: systemMod,
	}
}

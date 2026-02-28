// Package config
// 配置系统 — Node 自包含改造。
// Config 为可实例化的配置结构体，每个 Node 持有独立的 Config。
package config

import (
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/njtc406/emberengine/engine/pkg/config/remote"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/validate"
	"github.com/njtc406/viper"
)

// Config 是 Node 级配置实例。替代原来的全局 Conf 单例。
type Config struct {
	runtimeViper *viper.Viper
	clusterViper *viper.Viper

	NodeConf     *NodeConf       `binding:"required"` // 节点基础配置
	SystemLogger *log.LoggerConf `binding:"required"` // 系统日志
	ClusterConf  *ClusterConf    `binding:"required"` // 集群配置
	ServiceConf  *ServiceConf    `binding:"required"` // 服务配置

	serviceConfMap map[string]*ServiceConfig // 服务初始化配置（收归自 confMap.go）
	discoveryConf  map[string]interface{}    // 发现配置（收归自 confMap.go）
}

// NewConfig 创建一个新的空 Config 实例。
func NewConfig() *Config {
	return &Config{
		runtimeViper:   viper.New(),
		clusterViper:   viper.New(),
		serviceConfMap: make(map[string]*ServiceConfig),
		discoveryConf:  make(map[string]interface{}),
	}
}

// Load 解析配置文件（替代原来的全局 Init 函数）。
// 返回 error 而非 panic。
func (c *Config) Load(confPath string) error {
	fmt.Println("=============开始解析配置===================")
	if err := c.parseNodeConfig(confPath); err != nil {
		return fmt.Errorf("config.Load: %w", err)
	}
	c.initDir()
	fmt.Printf("Config: %s\n", c.String())
	fmt.Println("=============配置解析完成===================")
	return nil
}

// parseNodeConfig 解析本地配置文件（实例方法版本）
func (c *Config) parseNodeConfig(confPath string) error {
	// 解析配置路径
	envConfPath := os.Getenv("EMBER_CONF_PATH")
	if envConfPath != "" {
		confPath = envConfPath
	}
	if confPath == "" {
		confPath = defaultConfPath
	}

	// 1. 加载 .env 文件
	if err := godotenv.Load(path.Join(confPath, ".env")); err != nil {
		fmt.Println("No .env file found, fallback to system env")
		return fmt.Errorf("load .env: %w", err)
	}

	// 2. 读取原始配置文件（带 ${VAR}）
	rawYaml, err := os.ReadFile(path.Join(confPath, "node.yaml"))
	if err != nil {
		return fmt.Errorf("read node.yaml: %w", err)
	}

	// 3. 使用 os.ExpandEnv 替换变量
	resolvedYaml := os.ExpandEnv(string(rawYaml))

	c.runtimeViper.SetConfigType("yaml")

	if err = c.runtimeViper.ReadConfig(strings.NewReader(resolvedYaml)); err != nil {
		return fmt.Errorf("parse node config: %w", err)
	}
	if err = c.runtimeViper.Unmarshal(c); err != nil {
		return fmt.Errorf("unmarshal node config: %w", err)
	}

	// 绑定环境变量
	c.runtimeViper.SetEnvPrefix("EMBER_")
	c.runtimeViper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	c.runtimeViper.AutomaticEnv()

	if c.ServiceConf.OpenRemote {
		viper.RemoteConfig = &remote.Config{
			Endpoints: c.ClusterConf.ETCDConf.Endpoints,
			Username:  c.ClusterConf.ETCDConf.UserName,
			Password:  c.ClusterConf.ETCDConf.Password,
		}
		fmt.Println("=============使用远程配置===================")
	}

	// 合并 init 阶段注册的预初始化配置
	c.MergePreInitConf()

	// 解析启动服务
	if err = c.parseStartService(); err != nil {
		return err
	}

	// 解析服务配置
	if err = c.parseServiceConf(confPath); err != nil {
		return err
	}

	// 设置默认值
	c.setDefaultValues()

	if err = validate.Struct(c); err != nil {
		return fmt.Errorf("config validation: %w", validate.TransError(err, validate.ZH))
	}

	return nil
}

// initDir 创建必要的目录
func (c *Config) initDir() {
	createDirIfNotExists(c.NodeConf.PVPath)
	createDirIfNotExists(c.SystemLogger.Dir)
}

// parseStartService 解析启动的服务
func (c *Config) parseStartService() error {
	if !c.ServiceConf.OpenRemote {
		return nil
	}

	c.clusterViper.SetConfigType("yaml")
	err := c.clusterViper.AddRemoteProvider("etcd3",
		c.ClusterConf.ETCDConf.Endpoints[0],
		path.Join(c.ServiceConf.RemoteConfPath, startServiceConfName))
	if err != nil {
		return fmt.Errorf("add remote provider: %w", err)
	}

	if err = c.clusterViper.ReadRemoteConfig(); err != nil {
		return fmt.Errorf("read remote start service config: %w", err)
	}
	if err = c.clusterViper.Unmarshal(&c.ServiceConf); err != nil {
		return fmt.Errorf("unmarshal start service config: %w", err)
	}

	c.clusterViper.OnRemoteConfigChange(func() {
		if err := c.clusterViper.Unmarshal(&c.ServiceConf); err != nil {
			fmt.Println("clusterViper unmarshal failed:", err)
		}
		// TODO 执行配置变更函数
	})
	if err = c.clusterViper.WatchRemoteConfigOnChannel(); err != nil {
		return fmt.Errorf("watch remote config: %w", err)
	}
	return nil
}

// parseServiceConf 解析服务配置文件
func (c *Config) parseServiceConf(confPath string) error {
	servicesMap := make(map[string]*ServiceConfig)
	for name, v := range c.serviceConfMap {
		if v.CfgCreator == nil {
			continue
		}
		parser := viper.New()
		parser.SetConfigType("yaml")
		var err error
		if c.ServiceConf.OpenRemote {
			fileName := fmt.Sprintf("%s.%s", v.ConfName, v.ConfType)
			if err = parser.AddRemoteProvider("etcd3",
				c.ClusterConf.ETCDConf.Endpoints[0],
				path.Join(c.ServiceConf.RemoteConfPath, fileName)); err != nil {
				return fmt.Errorf("add remote provider for %s: %w", name, err)
			}
			err = parser.ReadRemoteConfig()
		} else {
			parser.SetConfigType(v.ConfType)
			parser.SetConfigName(v.ConfName)
			parser.AddConfigPath(confPath)
			err = parser.ReadInConfig()
		}

		if err != nil {
			fmt.Printf("[WARNING] ----->>>没有找到远程或者本地配置: %s\n", v.ConfName)
			continue
		}

		cfg := v.CfgCreator()
		if err = parser.Unmarshal(cfg); err != nil {
			return fmt.Errorf("unmarshal service config for %s: %w", name, err)
		}

		c.executeDefaultSet(parser)
		cf := *v
		cf.Cfg = cfg
		servicesMap[name] = &cf
	}
	c.ServiceConf.ServicesConfMap = servicesMap
	return nil
}

// executeDefaultSet 执行默认设置函数
func (c *Config) executeDefaultSet(parser *viper.Viper) {
	for _, v := range c.serviceConfMap {
		if v.DefaultSetFun != nil {
			v.DefaultSetFun(parser)
		}
	}
}

// setDefaultValues 设置默认值
func (c *Config) setDefaultValues() {
	c.runtimeViper.SetDefault("NodeConf", &NodeConf{
		SystemStatus: Debug,
		PVCPath:      def.DefaultPVCPath,
		PVPath:       def.DefaultPVPath,
		AntsPoolSize: def.DefaultAntsPoolSize,
		RpcMonitorConf: &RpcMonitorConf{
			MonitorTimerSize:  def.DefaultMonitorTimerSize,
			MonitorBucketSize: def.DefaultMonitorBucketSize,
			WaitBucketCount:   256,
			WaitBucketInitCap: 0,
		},
		TimingWheelConf: &TimingWheelConf{
			Interval:  time.Millisecond * 10,
			WheelSize: 1000,
		},
	})

	c.runtimeViper.SetDefault("SystemLogger", &log.LoggerConf{
		Dir:        path.Join(def.DefaultPVPath, "logs"),
		PrefixName: "system",
		Level:      "error",
		Stdout:     false,
		Caller:     true,
		FullCaller: false,
		Color:      false,
		Rotation: &log.RotationConf{
			MaxAge: 15 * 24 * time.Hour,
			Every:  24 * time.Hour,
		},
		Routing: &log.RoutingConf{
			AsyncMode: &log.AsyncMode{
				Enable: true,
				Config: &log.AsyncWriterConfig{
					BufferSize:    1024,
					FlushInterval: time.Second,
				},
			},
			Routes: []log.LevelRoute{
				{
					Name:   "info",
					Levels: log.AllLevelStrs,
				},
			},
		},
	})

	c.runtimeViper.SetDefault("ClusterConf", &ClusterConf{
		ETCDConf: &ETCDConf{
			Endpoints:   []string{"127.0.0.1:2379"},
			DialTimeout: 3 * time.Second,
			UserName:    "",
			Password:    "",
		},
		RPCServers: []*RPCServer{
			{
				Addr:   "0.0.0.0:6688",
				Protoc: "tcp",
				Type:   def.RpcTypeGrpc,
			},
		},
		DiscoveryType:  def.DiscoveryConfUseLocal,
		RemoteConfPath: "",
	})
}

// ── Config 实例方法（替代原包级函数）──

// IsDebug 返回是否为调试模式
func (c *Config) IsDebug() bool {
	if c == nil || c.NodeConf == nil {
		return false
	}
	return c.NodeConf.SystemStatus == Debug
}

// SetStatus 设置系统状态
func (c *Config) SetStatus(status string) {
	if c == nil {
		return
	}
	stat := strings.ToLower(status)
	if stat != Debug && stat != Release {
		return
	}
	if c.NodeConf == nil {
		c.NodeConf = &NodeConf{}
	}
	c.NodeConf.SystemStatus = stat
}

// GetStatus 返回系统状态
func (c *Config) GetStatus() string {
	if c == nil || c.NodeConf == nil {
		return ""
	}
	return c.NodeConf.SystemStatus
}

// GetDefaultRpcTimeout 获取 RPC 调用默认超时时间
func (c *Config) GetDefaultRpcTimeout() time.Duration {
	if c != nil && c.NodeConf != nil && c.NodeConf.RpcMonitorConf != nil {
		if c.NodeConf.RpcMonitorConf.DefaultRpcTimeout > 0 {
			return c.NodeConf.RpcMonitorConf.DefaultRpcTimeout
		}
	}
	return def.DefaultRpcTimeout
}

// GetCheckTimeoutInterval 获取 RPC 超时检查间隔
func (c *Config) GetCheckTimeoutInterval() time.Duration {
	if c != nil && c.NodeConf != nil && c.NodeConf.RpcMonitorConf != nil {
		if c.NodeConf.RpcMonitorConf.CheckTimeoutInterval > 0 {
			return c.NodeConf.RpcMonitorConf.CheckTimeoutInterval
		}
	}
	return def.DefaultCheckRpcCallTimeoutInterval
}

// ── 服务配置注册（收归自 confMap.go）──

// RegisterServiceConf 注册服务配置
func (c *Config) RegisterServiceConf(cfgs ...*ServiceConfig) {
	for _, cfg := range cfgs {
		c.serviceConfMap[cfg.ServiceName] = cfg
	}
}

// GetServiceConf 获取服务配置
func (c *Config) GetServiceConf(serviceName string) interface{} {
	cfg, ok := c.serviceConfMap[serviceName]
	if !ok {
		return nil
	}
	return cfg.CfgCreator
}

// RegisterDiscoveryConf 注册发现配置
func (c *Config) RegisterDiscoveryConf(name string, conf interface{}) {
	c.discoveryConf[name] = conf
}

// GetDiscoveryConf 获取发现配置
func (c *Config) GetDiscoveryConf(name string) interface{} {
	return c.discoveryConf[name]
}

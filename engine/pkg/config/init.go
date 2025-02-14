package config

import (
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/config/remote"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/validate"
	"github.com/njtc406/viper"
)

// TODO 这部分独立出来做成工具,包括node的配置,都注册进来,这样可以支持多种格式配置

var (
	runtimeViper = viper.New()
	clusterViper = viper.New()
	Conf         = new(conf)
)

const defaultConfPath = "./configs"
const startServiceConfName = "services.yaml"

// 配置初始化逻辑:
// 1. 解析节点基础配置
// 2. 根据服务配置决定使用本地或者远程
// 3. 读取服务配置
// 4. 节点会根据服务配置启动对应服务

func Init(confPath string) {
	fmt.Println("=============开始解析配置===================")
	// 解析配置
	parseNodeConfig(confPath)
	// 初始化目录
	initDir()
	fmt.Println("=============配置解析完成===================")
}

// parseNodeConfig 解析本地配置文件
func parseNodeConfig(confPath string) {
	// 解析配置路径
	envConfPath := os.Getenv("EMBER_CONF_PATH")
	if envConfPath != "" {
		confPath = envConfPath
	}
	if confPath == "" {
		confPath = defaultConfPath
	}

	runtimeViper.SetConfigType("yaml")
	runtimeViper.SetConfigName("node")
	runtimeViper.AddConfigPath(confPath)

	// 解析节点配置
	parseSystemConfig(runtimeViper, Conf)

	// 绑定环境变量(这里需要注意的是,如果在配置中已经配置了,环境变量的优先级是低的一方,即不会覆盖已有配置,所以如果需要使用环境变量配置,就不要配置)
	runtimeViper.SetEnvPrefix("EMBER_")
	runtimeViper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	runtimeViper.AutomaticEnv()

	if Conf.ServiceConf.OpenRemote {
		// 从远程读取启动服务配置
		// 设置viper的远程配置
		viper.RemoteConfig = &remote.Config{
			Endpoints: Conf.ClusterConf.ETCDConf.Endpoints,
			Username:  Conf.ClusterConf.ETCDConf.UserName,
			Password:  Conf.ClusterConf.ETCDConf.Password,
		}

		fmt.Println("=============使用远程配置===================")
	}

	// 解析启动服务(如果本地没有配置,就从远程读取)
	parseStartService()

	// 解析服务配置
	parseServiceConf(confPath)

	// 设置默认值
	setDefaultValues()

	if err := validate.Struct(Conf); err != nil {
		panic(validate.TransError(err, validate.ZH))
	}
}

// initDir 创建必要的目录
func initDir() {
	createDirIfNotExists(Conf.NodeConf.PVPath)
	createDirIfNotExists(Conf.NodeConf.PVPath)
	createDirIfNotExists(Conf.SystemLogger.Path)
}

// createDirIfNotExists 创建目录
func createDirIfNotExists(dir string) {
	if err := os.MkdirAll(dir, 0644); err != nil {
		panic(err)
	}
}

// setDefaultValues 设置默认值
func setDefaultValues() {
	// 默认基础配置
	runtimeViper.SetDefault("NodeConf", &NodeConf{
		SystemStatus:      Debug,
		PVCPath:           def.DefaultPVCPath,
		PVPath:            def.DefaultPVPath,
		ProfilerInterval:  0, // 默认不开启
		AntsPoolSize:      def.DefaultAntsPoolSize,
		MonitorTimerSize:  def.DefaultMonitorTimerSize,
		MonitorBucketSize: def.DefaultMonitorBucketSize,
	})

	// 日志默认配置
	runtimeViper.SetDefault("SystemLogger", &log.LoggerConf{
		Path:         path.Join(def.DefaultPVPath, "logs"),
		Name:         "system",
		Level:        "error",
		Caller:       true,
		FullCaller:   false,
		Color:        false,
		MaxAge:       time.Hour * 24 * 15,
		RotationTime: time.Hour * 24,
	})

	// 默认集群配置
	runtimeViper.SetDefault("ClusterConf", &ClusterConf{
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

// parseServiceConf 解析服务配置文件
func parseServiceConf(confPath string) {
	servicesMap := make(map[string]*ServiceConfig)
	for name, v := range serviceConfMap {
		if v.CfgCreator == nil {
			continue
		}
		parser := viper.New()
		parser.SetConfigType("yaml")
		if Conf.ServiceConf.OpenRemote {
			// 使用远程服务配置
			fileName := fmt.Sprintf("%s.%s", v.ConfName, v.ConfType)
			if err := parser.AddRemoteProvider("etcd3", Conf.ClusterConf.ETCDConf.Endpoints[0], path.Join(Conf.ServiceConf.RemoteConfPath, fileName)); err != nil {
				panic(err)
			}
			if err := parser.ReadRemoteConfig(); err != nil {
				panic(err)
			}
		} else {
			parser.SetConfigType(v.ConfType)
			parser.SetConfigName(v.ConfName)
			parser.AddConfigPath(confPath)
			if err := parser.ReadInConfig(); err != nil {
				panic(err)
			}
		}
		cfg := v.CfgCreator()
		if err := parser.Unmarshal(cfg); err != nil {
			panic(err)
		}

		executeDefaultSet(parser)
		cf := *v
		cf.Cfg = cfg
		servicesMap[name] = &cf
	}
	Conf.ServiceConf.ServicesConfMap = servicesMap
}

func parseStartService() {
	if Conf.ServiceConf.OpenRemote {
		clusterViper.SetConfigType("yaml")
		err := clusterViper.AddRemoteProvider("etcd3", Conf.ClusterConf.ETCDConf.Endpoints[0], path.Join(Conf.ServiceConf.RemoteConfPath, startServiceConfName))
		if err != nil {
			panic(err)
		}

		if err = clusterViper.ReadRemoteConfig(); err != nil {
			panic(err)
		}
		if err = clusterViper.Unmarshal(&Conf.ServiceConf); err != nil {
			panic(err)
		}
		clusterViper.OnRemoteConfigChange(func() {
			if err = clusterViper.Unmarshal(&Conf.ServiceConf); err != nil {
				fmt.Println("clusterViper unmarshal failed:", err)
			}

			// TODO 执行配置变更函数
		})
		if err = clusterViper.WatchRemoteConfigOnChannel(); err != nil {
			panic(err)
		}
	}
}

// listenConfChange 监听配置文件变更
//func listenConfChange(parser *viper.Viper, onChangeFun func()) {
//	parser.WatchConfig()
//	var callbackTimer *time.Timer
//	parser.OnConfigChange(func(in fsnotify.Event) {
//		fmt.Printf("配置文件变更: %s", in.ServiceName)
//		if callbackTimer != nil {
//			callbackTimer.Stop()
//		}
//		// 由于某些viper的问题,这个事件可能会多次调用,所以这里做一个延迟,避免一次改动调用多次回调
//		callbackTimer = time.AfterFunc(time.Millisecond*50, func() {
//			onChangeFun()
//		})
//	})
//}

// executeDefaultSet 执行默认设置函数
func executeDefaultSet(parser *viper.Viper) {
	for _, v := range serviceConfMap {
		if v.DefaultSetFun != nil {
			v.DefaultSetFun(parser)
		}
	}
}

// parseSystemConfig 解析系统配置
func parseSystemConfig(parser *viper.Viper, c interface{}) {
	if err := parser.ReadInConfig(); err != nil {
		panic(err)
	}
	if err := parser.Unmarshal(c); err != nil {
		panic(err)
	}
}

// IsDebug 返回是否为调试模式
func IsDebug() bool {
	return Conf.NodeConf.SystemStatus == Debug
}

// SetStatus 设置系统状态
func SetStatus(status string) {
	stat := strings.ToLower(status)
	if stat != Debug && stat != Release {
		return
	}

	Conf.NodeConf.SystemStatus = stat
}

func GetStatus() string {
	return Conf.NodeConf.SystemStatus
}

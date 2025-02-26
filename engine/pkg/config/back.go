// Package config
// @Title  title
// @Description  desc
// @Author  yr  2025/2/7
// @Update  yr  2025/2/7
package config

// TODO 这是一个gpt提供的配置解析方案,和原来的一样,但是整理过一些逻辑,还未验证,后续需要验证一下,本地的可以是用,远程还未验证

//var (
//	runtimeViper = viper.New()
//	clusterViper = viper.New()
//	Conf         = new(conf)
//)
//
//const defaultConfPath = "./configs"
//const startServiceConfName = "services.yaml"
//
//// Init 配置初始化
//func Init(confPath string) {
//	fmt.Println("=============开始解析配置===================")
//
//	// 解析本地配置
//	if err := parseNodeConfig(confPath); err != nil {
//		panic(err)
//	}
//
//	// 初始化目录
//	initDir()
//
//	// 解析启动的服务
//	if err := parseStartService(); err != nil {
//		panic(err)
//	}
//
//	// 解析服务配置
//	if err := parseServiceConf(confPath); err != nil {
//		panic(err)
//	}
//
//	fmt.Println("=============配置解析完成===================")
//}
//
//// parseNodeConfig 解析本地配置文件
//func parseNodeConfig(confPath string) error {
//	// 解析配置路径
//	envConfPath := os.Getenv("EMBER_CONF_PATH")
//	if envConfPath != "" {
//		confPath = envConfPath
//	}
//	if confPath == "" {
//		confPath = defaultConfPath
//	}
//
//	runtimeViper.SetConfigType("yaml")
//	runtimeViper.SetConfigName("node")
//	runtimeViper.AddConfigPath(confPath)
//
//	if err := runtimeViper.ReadInConfig(); err != nil {
//		return fmt.Errorf("failed to read node config: %w", err)
//	}
//	if err := runtimeViper.Unmarshal(Conf); err != nil {
//		return fmt.Errorf("failed to unmarshal node config: %w", err)
//	}
//
//	// 绑定环境变量
//	runtimeViper.SetEnvPrefix("EMBER")
//	runtimeViper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
//	runtimeViper.AutomaticEnv()
//
//	// 如果需要远程配置，解析远程配置
//	if Conf.ServiceConf.OpenRemote {
//		if err := parseRemoteConfig(); err != nil {
//			return err
//		}
//	}
//
//	// 设置默认值
//	setDefaultValues()
//
//	// 校验配置
//	if err := validate.Struct(Conf); err != nil {
//		return fmt.Errorf("config validation failed: %w", err)
//	}
//	return nil
//}
//
//// parseRemoteConfig 解析远程配置
//func parseRemoteConfig() error {
//	// 设置远程配置
//	viper.RemoteConfig = &remote.Config{
//		Endpoints: Conf.ClusterConf.ETCDConf.Endpoints,
//		Username:  Conf.ClusterConf.ETCDConf.UserName,
//		Password:  Conf.ClusterConf.ETCDConf.Password,
//	}
//
//	fmt.Println("=============使用远程配置===================")
//	return nil
//}
//
//// initDir 创建必要的目录
//func initDir() {
//	createDirIfNotExists(Conf.NodeConf.PVPath)
//	createDirIfNotExists(Conf.SystemLogger.Path)
//}
//
//// createDirIfNotExists 创建目录
//func createDirIfNotExists(dir string) {
//	if err := os.MkdirAll(dir, 0755); err != nil { // 0755 避免权限问题
//		panic(err)
//	}
//}
//
//// parseStartService 解析启动的服务
//func parseStartService() error {
//	if !Conf.ServiceConf.OpenRemote {
//		return nil
//	}
//
//	clusterViper.SetConfigType("yaml")
//	err := clusterViper.AddRemoteProvider("etcd3", Conf.ClusterConf.ETCDConf.Endpoints[0], path.Join(Conf.ServiceConf.RemoteConfPath, startServiceConfName))
//	if err != nil {
//		return fmt.Errorf("failed to add remote provider: %w", err)
//	}
//
//	if err = clusterViper.ReadRemoteConfig(); err != nil {
//		return fmt.Errorf("failed to read remote start service config: %w", err)
//	}
//	if err = clusterViper.Unmarshal(&Conf.ServiceConf); err != nil {
//		return fmt.Errorf("failed to unmarshal start service config: %w", err)
//	}
//
//	// 监听远程配置变更
//	clusterViper.OnRemoteConfigChange(func() {
//		if err := clusterViper.Unmarshal(&Conf.ServiceConf); err != nil {
//			fmt.Println("Failed to unmarshal updated config:", err)
//		}
//		// TODO: 执行配置变更函数
//	})
//
//	if err = clusterViper.WatchRemoteConfigOnChannel(); err != nil {
//		return fmt.Errorf("failed to watch remote config: %w", err)
//	}
//
//	return nil
//}
//
//// parseServiceConf 解析服务配置
//func parseServiceConf(confPath string) error {
//	servicesMap := make(map[string]*ServiceConfig)
//
//	for name, v := range serviceConfMap {
//		if v.CfgCreator == nil {
//			continue
//		}
//		parser := viper.New()
//		parser.SetConfigType("yaml")
//
//		// 选择远程 or 本地
//		if Conf.ServiceConf.OpenRemote {
//			fileName := fmt.Sprintf("%s.%s", v.ConfName, v.ConfType)
//			remotePath := path.Join(Conf.ServiceConf.RemoteConfPath, fileName)
//			if err := parser.AddRemoteProvider("etcd3", Conf.ClusterConf.ETCDConf.Endpoints[0], remotePath); err != nil {
//				return fmt.Errorf("failed to add remote provider: %w", err)
//			}
//			if err := parser.ReadRemoteConfig(); err != nil {
//				return fmt.Errorf("failed to read remote config: %w", err)
//			}
//		} else {
//			parser.SetConfigType(v.ConfType)
//			parser.SetConfigName(v.ConfName)
//			parser.AddConfigPath(confPath)
//			if err := parser.ReadInConfig(); err != nil {
//				return fmt.Errorf("failed to read local service config: %w", err)
//			}
//		}
//
//		cfg := v.CfgCreator()
//		if err := parser.Unmarshal(cfg); err != nil {
//			return fmt.Errorf("failed to unmarshal service config for %s: %w", name, err)
//		}
//
//		// 执行默认设置
//		executeDefaultSet(parser)
//
//		cf := *v
//		cf.Cfg = cfg
//		servicesMap[name] = &cf
//	}
//
//	Conf.ServiceConf.ServicesConfMap = servicesMap
//	return nil
//}
//
//// executeDefaultSet 执行默认设置函数
//func executeDefaultSet(parser *viper.Viper) {
//	for _, v := range serviceConfMap {
//		if v.DefaultSetFun != nil {
//			v.DefaultSetFun(parser)
//		}
//	}
//}

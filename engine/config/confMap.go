// Package config
// @Title  title
// @Description  desc
// @Author  yr  2024/11/28
// @Update  yr  2024/11/28
package config

var (
	serviceConfMap = make(map[string]*ServiceConfig) // 服务初始化配置
	discoveryConf  = make(map[string]interface{})
)

func RegisterServiceConf(cfgs ...*ServiceConfig) {
	for _, cfg := range cfgs {
		serviceConfMap[cfg.ServiceName] = cfg
	}
}

func GetServiceConf(serviceName string) interface{} {
	cfg, ok := serviceConfMap[serviceName]
	if !ok {
		return nil
	}
	return cfg.CfgCreator
}

func RegisterDiscoveryConf(name string, conf interface{}) {
	discoveryConf[name] = conf
}

func GetDiscoveryConf(name string) interface{} {
	return discoveryConf[name]
}

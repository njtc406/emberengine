// Package config
// @Title  title
// @Description  desc
// @Author  yr  2024/11/28
// @Update  yr  2024/11/28
package config

// ── 以下全局函数仅供临时兼容 ──
// serviceConfMap 和 discoveryConf 已收归到 Config 结构体内。
// 以下包级函数委托到 Conf 实例。

// RegisterServiceConf 包级兼容函数。
// Deprecated: 请使用 Config.RegisterServiceConf()。
func RegisterServiceConf(cfgs ...*ServiceConfig) {
	if Conf != nil {
		Conf.RegisterServiceConf(cfgs...)
		return
	}
	// 如果在 Node.Start 之前调用（init 阶段），存入临时全局 map
	initServiceConfMap.register(cfgs...)
}

// GetServiceConf 包级兼容函数。
// Deprecated: 请使用 Config.GetServiceConf()。
func GetServiceConf(serviceName string) interface{} {
	if Conf != nil {
		return Conf.GetServiceConf(serviceName)
	}
	return nil
}

// RegisterDiscoveryConf 包级兼容函数。
// Deprecated: 请使用 Config.RegisterDiscoveryConf()。
func RegisterDiscoveryConf(name string, conf interface{}) {
	if Conf != nil {
		Conf.RegisterDiscoveryConf(name, conf)
		return
	}
	initDiscoveryConfMap[name] = conf
}

// GetDiscoveryConf 包级兼容函数。
// Deprecated: 请使用 Config.GetDiscoveryConf()。
func GetDiscoveryConf(name string) interface{} {
	if Conf != nil {
		return Conf.GetDiscoveryConf(name)
	}
	return nil
}

// ── init 阶段临时存储 ──
// 某些包的 init() 会在 Config 创建前注册服务配置/发现配置。
// 这些临时 map 会在 Config.Load 时被合并。

var (
	initServiceConfMap   = &preInitServiceMap{m: make(map[string]*ServiceConfig)}
	initDiscoveryConfMap = make(map[string]interface{})
)

type preInitServiceMap struct {
	m map[string]*ServiceConfig
}

func (p *preInitServiceMap) register(cfgs ...*ServiceConfig) {
	for _, cfg := range cfgs {
		p.m[cfg.ServiceName] = cfg
	}
}

// MergePreInitConf 将 init 阶段注册的配置合并到 Config 实例中。
// 由 Config.Load 内部调用。
func (c *Config) MergePreInitConf() {
	for name, cfg := range initServiceConfMap.m {
		if _, exists := c.serviceConfMap[name]; !exists {
			c.serviceConfMap[name] = cfg
		}
	}
	for name, conf := range initDiscoveryConfMap {
		if _, exists := c.discoveryConf[name]; !exists {
			c.discoveryConf[name] = conf
		}
	}
}

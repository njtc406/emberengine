// Package config
// @Title  title
// @Description  desc
// @Author  yr  2024/11/28
// @Update  yr  2024/11/28
package config

// ── 包级预注册函数（仅用于 init 阶段） ──
// serviceConfMap 和 discoveryConf 已收归到 Config 结构体内。
// 这些函数仅写入 init 阶段临时缓存，并由 Config.Load 合并。

// RegisterServiceConf 在 init 阶段预注册服务配置。
func RegisterServiceConf(cfgs ...*ServiceConfig) {
	initServiceConfMap.register(cfgs...)
}

// ── init 阶段临时存储 ──
// 某些包的 init() 会在 Config 创建前注册服务配置/发现配置。
// 这些临时 map 会在 Config.Load 时被合并。

var (
	initServiceConfMap = &preInitServiceMap{m: make(map[string]*ServiceConfig)}
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
}

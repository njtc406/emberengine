package config

import (
	"os"
	"strings"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/def"
)

//	以下为临时全局兼容
//
// Conf 是临时全局配置指针，仅供尚未完成 Node 化改造的消费方使用。
// 后续 Phase 中会逐步移除所有对 Conf 的引用。
// Deprecated: 请通过 NodeContext.GetConfig() 获取。
var Conf *Config

// SetConf 由 Node.Start 调用，设置全局 Conf（临时兼容）。
// Deprecated: 后续 Phase 删除。
func SetConf(c *Config) { Conf = c }

// Init 已废弃。使用 NewConfig().Load(confPath) 替代。
// Deprecated: 后续 Phase 删除。
func Init(confPath string) {
	c := NewConfig()
	if err := c.Load(confPath); err != nil {
		panic(err)
	}
	Conf = c
}

//  包级辅助函数（无状态，保留）

const defaultConfPath = "./configs"
const startServiceConfName = "services.yaml"

func createDirIfNotExists(dir string) {
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0644); err != nil {
		panic(err)
	}
}

// IsDebug 包级兼容函数。
// Deprecated: 请使用 Config.IsDebug()。
func IsDebug() bool {
	if Conf == nil || Conf.NodeConf == nil {
		return false
	}
	return Conf.NodeConf.SystemStatus == Debug
}

// SetStatus 包级兼容函数。
// Deprecated: 请使用 Config.SetStatus()。
func SetStatus(status string) {
	if Conf == nil {
		return
	}
	stat := strings.ToLower(status)
	if stat != Debug && stat != Release {
		return
	}
	if Conf.NodeConf == nil {
		Conf.NodeConf = &NodeConf{}
	}
	Conf.NodeConf.SystemStatus = stat
}

// GetStatus 包级兼容函数。
// Deprecated: 请使用 Config.GetStatus()。
func GetStatus() string {
	if Conf == nil || Conf.NodeConf == nil {
		return ""
	}
	return Conf.NodeConf.SystemStatus
}

// GetDefaultRpcTimeout 包级兼容函数。
// Deprecated: 请使用 Config.GetDefaultRpcTimeout()。
func GetDefaultRpcTimeout() time.Duration {
	if Conf != nil && Conf.NodeConf != nil && Conf.NodeConf.RpcMonitorConf != nil {
		if Conf.NodeConf.RpcMonitorConf.DefaultRpcTimeout > 0 {
			return Conf.NodeConf.RpcMonitorConf.DefaultRpcTimeout
		}
	}
	return def.DefaultRpcTimeout
}

// GetCheckTimeoutInterval 包级兼容函数。
// Deprecated: 请使用 Config.GetCheckTimeoutInterval()。
func GetCheckTimeoutInterval() time.Duration {
	if Conf != nil && Conf.NodeConf != nil && Conf.NodeConf.RpcMonitorConf != nil {
		if Conf.NodeConf.RpcMonitorConf.CheckTimeoutInterval > 0 {
			return Conf.NodeConf.RpcMonitorConf.CheckTimeoutInterval
		}
	}
	return def.DefaultCheckRpcCallTimeoutInterval
}

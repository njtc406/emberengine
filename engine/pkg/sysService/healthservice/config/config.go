package config

import (
	"time"

	"github.com/njtc406/viper"
)

// HealthConf 运维端点（/health, /ready, /metrics）配置
type HealthConf struct {
	Addr              string        `binding:"required"` // 监听地址
	ReadHeaderTimeout time.Duration `binding:""`         // 读取请求头超时
	IdleTimeout       time.Duration `binding:""`         // 空闲连接超时
}

// SetHealthConfDefault 设置默认值
func SetHealthConfDefault(parser *viper.Viper) {
	parser.SetDefault("Addr", "0.0.0.0:9090")
	parser.SetDefault("ReadHeaderTimeout", 10*time.Second)
	parser.SetDefault("IdleTimeout", 30*time.Second)
}

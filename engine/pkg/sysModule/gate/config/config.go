// Package config
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/8/14 0014 0:14
// 最后更新:  yr  2025/8/14 0014 0:14
package config

import (
	"time"

	"github.com/njtc406/emberengine/engine/pkg/utils/httpx"
	"github.com/njtc406/viper"
)

// RestartPolicy 配置 Gate 监听失败后的重启策略。
type RestartPolicy struct {
	Enable         bool          `binding:""`      // 是否启用重启(默认true)
	MaxRestart     int           `binding:"min=0"` // 最大重启次数(默认5)
	InitialBackoff time.Duration `binding:"min=0"` // 初始退避时间(默认500ms)
	MaxBackoff     time.Duration `binding:"min=0"` // 最大退避时间(默认10s)
}

type GateService struct {
	Type           string         `binding:"required,oneof=ws http tcp udp"`
	WSServerConf   *WSServerConf  `binding:""`
	HttpServerConf *httpx.Conf    `binding:""`
	TcpServerConf  *TcpServerConf `binding:""`
	UdpServerConf  *UdpServerConf `binding:""`
	RestartPolicy  *RestartPolicy `binding:""` // 重启策略(默认启用,最多重启5次)
}

type WSServerConf struct {
	Router         string `binding:"required"`
	JWTSecret      string
	LittleEndian   bool     //是否小端序
	AllowedOrigins []string // WebSocket 允许的 Origin 列表；为空时默认同源；包含 "*" 允许所有
	HttpConf       *httpx.Conf
}

type TcpServerConf struct {
	Addr         string
	LittleEndian bool //是否小端序
}

type UdpServerConf struct {
	Addr         string
	LittleEndian bool //是否小端序
}

func SetChatServiceConfDefault(parser *viper.Viper) {
	parser.SetDefault("Type", "ws") // 默认使用websocket
	parser.SetDefault("WSServerConf", &WSServerConf{
		Router:       "/ws",
		JWTSecret:    "",
		LittleEndian: false,
		HttpConf: &httpx.Conf{
			Addr: ":8080",
		},
	})
}

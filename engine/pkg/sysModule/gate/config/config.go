// Package config
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/8/14 0014 0:14
// 最后更新:  yr  2025/8/14 0014 0:14
package config

import (
	"github.com/njtc406/emberengine/engine/pkg/utils/httpx"
	"github.com/njtc406/viper"
)

type GateService struct {
	Type           string         `binding:"required,oneof=ws http tcp udp"`
	WSServerConf   *WSServerConf  `binding:""`
	HttpServerConf *httpx.Conf    `binding:""`
	TcpServerConf  *TcpServerConf `binding:""`
	UdpServerConf  *UdpServerConf `binding:""`
}

type WSServerConf struct {
	Router       string `binding:"required"`
	LittleEndian bool   //是否小端序
	HttpConf     *httpx.Conf
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
		LittleEndian: false,
		HttpConf: &httpx.Conf{
			Addr: ":8080",
		},
	})
}

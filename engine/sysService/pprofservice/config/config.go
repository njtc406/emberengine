// Package config
// @Title  请填写文件名称（需要改）
// @Description  请填写文件描述（需要改）
// @Author  yr  2024/8/21 下午5:03
// @Update  yr  2024/8/21 下午5:03
package config

import (
	"github.com/njtc406/emberengine/engine/sysModule/httpmodule"
	"github.com/njtc406/viper"
	"time"
)

type PprofConf struct {
	PprofConf *httpmodule.Conf `binding:"required"`
}

func SetPprofConfDefault(parser *viper.Viper) {
	parser.SetDefault("PprofConf", &httpmodule.Conf{
		Addr:              "0.0.0.0:99",
		ReadHeaderTimeout: time.Second * 10,
		IdleTimeout:       time.Second * 30,
		CachePath:         "",
		ResourceRootPath:  "",
		HttpDir:           "",
		StaticDir:         "",
		Auth:              false,
		Account:           nil,
	})
}

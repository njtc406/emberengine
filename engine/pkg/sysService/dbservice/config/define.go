package config

import (
	"github.com/njtc406/emberengine/engine/pkg/sysModule/mysqlmodule"
	"github.com/njtc406/viper"
	"github.com/redis/go-redis/v9"
	"time"
)

type DBService struct {
	MysqlConf *mysqlmodule.Conf `binding:"required"` // mysql配置
	RedisConf *redis.Options    `binding:"required"` // redis配置
}

func DefaultDBService(parser *viper.Viper) {
	parser.SetDefault("MysqlConf", &mysqlmodule.Conf{
		UserName:           "",
		Passwd:             "",
		Net:                "tcp",
		Addr:               "0.0.0.0:3306",
		DBNamePrefix:       "ember_",
		TimeZone:           "Local",
		Timeout:            time.Second * 10,
		ReadTimeout:        time.Second * 10,
		WriteTimeout:       time.Second * 10,
		SetConnMaxIdleTime: 0,
		SetConnMaxLifetime: 0,
		SetMaxIdleConns:    0,
		SetMaxOpenConns:    0,
	})
}

// Package memdbx
// @Title  title
// @Description  基于sqlite内存模式的数据库
// @Author  yr  2025/6/19
// @Update  yr  2025/6/19
package memdbx

import (
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	syslog "log"
	"time"
)

var memDB *gorm.DB

func Start() {
	slowLogger := logger.New(
		syslog.New(log.SysLogger.GetOutput(), "\n", syslog.LstdFlags),
		logger.Config{
			// 设定慢查询时间阈值为 默认值：1 * time.Millisecond
			SlowThreshold: 1 * time.Millisecond,
			// 设置日志级别
			LogLevel: logger.Warn,
			Colorful: true,
		},
	)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                 slowLogger,
		SkipDefaultTransaction: true,
	})
	if err != nil {
		log.SysLogger.Panicf("memdb init failed, err:%v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		log.SysLogger.Panicf("memdb get DB failed, err:%v", err)
	}

	sqlDB.SetMaxOpenConns(1) // 限制为单连接,防止出现问题,如果需要多连接,需要设置 sqlite.Open("file::memory:?cache=shared")
	sqlDB.SetMaxIdleConns(1)

	memDB = db
}

func GetDB() *gorm.DB {
	return memDB
}

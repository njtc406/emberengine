// Package mysqlmodule
// @Title  mysql模块
// @Description  mysql模块
// @Author  yr  2024/7/25 下午3:12
// @Update  yr  2024/7/25 下午3:12
package mysqlmodule

import (
	"fmt"
	syslog "log"
	"os"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/core"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Conf struct {
	UserName           string        `binding:"required"` // 数据库用户名
	Passwd             string        `binding:"required"` // 数据库密码
	Net                string        `binding:"required"` // 连接方式
	Addr               string        `binding:"required"` // 数据库地址
	DBNamePrefix       string        `binding:"required"` // 使用的数据库名的前缀,真正的名字是 DBNamePrefix + "_" + nodeID
	TimeZone           string        `binding:"required"` // 时区
	Timeout            time.Duration `binding:""`         // 连接超时时间
	ReadTimeout        time.Duration `binding:""`         // 读取超时时间
	WriteTimeout       time.Duration `binding:""`         // 写超时时间
	SetConnMaxIdleTime time.Duration `binding:""`         // 连接最大空闲时间
	SetConnMaxLifetime time.Duration `binding:""`         // 连接最大生命周期
	SetMaxIdleConns    int           `binding:""`         // 最大空闲连接数
	SetMaxOpenConns    int           `binding:""`         // 最大打开连接数
}

var tables []interface{}

func RegisterTable(dst ...interface{}) {
	tables = append(tables, dst...)
}

type Callback func(tx *gorm.DB, args ...interface{}) (interface{}, error)
type TransactionCallback func(tx *gorm.DB) error

type MysqlModule struct {
	core.Module

	client *gorm.DB
}

func NewMysqlModule() *MysqlModule {
	return &MysqlModule{}
}

func (m *MysqlModule) GetClient() *gorm.DB {
	return m.client
}

func (m *MysqlModule) getDBName(prefix string, id int32) string {
	return fmt.Sprintf("%s_%d", prefix, id)
}

func (m *MysqlModule) OnInit() error {
	// 这个函数是在InitConn之后调用的

	// 初始化表
	if err := m.client.AutoMigrate(tables...); err != nil {
		return err
	}
	return nil
}

func (m *MysqlModule) checkDataBase(conf *Conf, id int32) error {
	db, err := m.initDB(conf, id, true)
	if err != nil {
		log.SysLogger.Panic(err)
	}
	// 检查数据库是否存在
	var count int
	dbName := m.getDBName(conf.DBNamePrefix, id)
	db.Raw("SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name = ?", dbName).Scan(&count)
	if count == 0 {
		// 数据库不存在，创建它
		sql := fmt.Sprintf(`create database if not exists %s default charset utf8mb4 collate utf8mb4_unicode_ci`,
			dbName)
		if err = db.Exec(sql).Error; err != nil {
			return err
		}
	}

	return nil
}

func (m *MysqlModule) initDB(conf *Conf, id int32, withoutDBName bool) (*gorm.DB, error) {
	slowLogger := logger.New(
		syslog.New(log.SysLogger.GetOutput(), "\n", syslog.LstdFlags),
		logger.Config{
			// 设定慢查询时间阈值为 默认值：200 * time.Millisecond
			SlowThreshold: 200 * time.Millisecond,
			// 设置日志级别
			LogLevel: logger.Warn,
			Colorful: true,
		},
	)

	var dsn string
	if withoutDBName {
		dsn = fmt.Sprintf("%s:%s@tcp(%s)/?charset=utf8&parseTime=True&loc=%s",
			conf.UserName,
			conf.Passwd,
			conf.Addr,
			conf.TimeZone,
		)
	} else {
		dsn = fmt.Sprintf("%s:%s@tcp(%s)/%s?charset=utf8&parseTime=True&loc=%s",
			conf.UserName,
			conf.Passwd,
			conf.Addr,
			m.getDBName(conf.DBNamePrefix, id),
			conf.TimeZone,
		)
	}

	log.SysLogger.Infof("mysql connect : %s", dsn)

	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                     dsn,
		DontSupportRenameColumn: true,
		//SkipInitializeWithVersion: false, // 根据数据库版本自动配置
	}), &gorm.Config{
		Logger:      slowLogger,
		PrepareStmt: true, // 开启预处理语句
		//SkipDefaultTransaction: true, // 跳过默认事务
	})
	if err != nil {
		return nil, err
	}

	return db, nil
}

// InitConn 初始化连接
func (m *MysqlModule) InitConn(conf *Conf, id int32) {
	dbName := os.Getenv("DB_NAME")
	if dbName != "" {
		conf.DBNamePrefix = dbName
	}

	if err := m.checkDataBase(conf, id); err != nil {
		log.SysLogger.Panic(err)
	}
	client, err := m.initDB(conf, id, false)
	if err != nil {
		log.SysLogger.Panic(err)
	}

	sqlDB, _ := client.DB()
	sqlDB.SetConnMaxIdleTime(time.Minute * 5)
	sqlDB.SetConnMaxLifetime(time.Minute * 10)

	var idleRate int
	var openRate int
	if runtime.NumCPU() > 4 {
		idleRate = 4
		openRate = 8
	} else {
		idleRate = 2
		openRate = 4
	}

	sqlDB.SetMaxIdleConns(runtime.NumCPU() * idleRate)
	sqlDB.SetMaxOpenConns(runtime.NumCPU() * openRate)

	m.client = client
}

func (m *MysqlModule) ExecuteFun(f Callback, args ...interface{}) (interface{}, error) {
	defer func() {
		if r := recover(); r != nil {
			log.SysLogger.Errorf("mysql execute function panic: %v\ntrace:%s", r, debug.Stack())
		}
	}()

	return f(m.client, args...)
}

func (m *MysqlModule) ExecuteTransaction(funs ...TransactionCallback) error {
	if err := m.client.Transaction(func(tx *gorm.DB) error {
		for _, f := range funs {
			if err := f(tx); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

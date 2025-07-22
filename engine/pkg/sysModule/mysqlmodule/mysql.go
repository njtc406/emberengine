// Package mysqlmodule
// @Title  mysql模块
// @Description  mysql模块
// @Author  yr  2024/7/25 下午3:12
// @Update  yr  2024/7/25 下午3:12
package mysqlmodule

import (
	"fmt"
	syslog "log"
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
	TimeZone           string        `binding:"required"` // 时区
	Timeout            time.Duration `binding:""`         // 连接超时时间
	ReadTimeout        time.Duration `binding:""`         // 读取超时时间
	WriteTimeout       time.Duration `binding:""`         // 写超时时间
	SetConnMaxIdleTime time.Duration `binding:""`         // 连接最大空闲时间
	SetConnMaxLifetime time.Duration `binding:""`         // 连接最大生命周期
	SetMaxIdleConns    int           `binding:""`         // 最大空闲连接数
	SetMaxOpenConns    int           `binding:""`         // 最大打开连接数
}

type Callback func(tx *gorm.DB, args ...interface{}) (interface{}, error)
type TransactionCallback func(tx *gorm.DB) error

type MysqlModule struct {
	core.Module

	conf   *Conf
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
	return nil
}

func (m *MysqlModule) initConn(database *string) (*gorm.DB, error) {
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
	if database == nil {
		dsn = fmt.Sprintf("%s:%s@tcp(%s)/?charset=utf8&parseTime=True&loc=%s",
			m.conf.UserName,
			m.conf.Passwd,
			m.conf.Addr,
			m.conf.TimeZone,
		)
	} else {
		dsn = fmt.Sprintf("%s:%s@tcp(%s)/%s?charset=utf8&parseTime=True&loc=%s",
			m.conf.UserName,
			m.conf.Passwd,
			m.conf.Addr,
			*database,
			m.conf.TimeZone,
		)
	}

	log.SysLogger.Infof("mysql connect : %s", dsn)

	return gorm.Open(mysql.New(mysql.Config{
		DSN:                     dsn,
		DontSupportRenameColumn: true,
		//SkipInitializeWithVersion: false, // 根据数据库版本自动配置

	}), &gorm.Config{
		Logger:      slowLogger,
		PrepareStmt: true, // 开启预处理语句
		//SkipDefaultTransaction: true, // 跳过默认事务
	})
}

// Init 初始化连接
func (m *MysqlModule) Init(conf *Conf) {
	m.conf = conf

	client, err := m.initConn(nil)
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

func (m *MysqlModule) ApiMysqlExecuteFun(f Callback, args ...interface{}) (interface{}, error) {
	defer func() {
		if r := recover(); r != nil {
			log.SysLogger.Errorf("mysql execute function panic: %v\ntrace:%s", r, debug.Stack())
		}
	}()
	return f(m.client, args...)
}

func (m *MysqlModule) ApiMysqlExecuteTransaction(funs ...TransactionCallback) error {
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

func (m *MysqlModule) initDatabase(db *gorm.DB, database string) error {
	// 检查数据库是否存在
	var count int
	db.Raw("SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name = ?", database).Scan(&count)
	if count == 0 {
		// 数据库不存在，创建它
		sql := fmt.Sprintf(`create database if not exists %s default charset utf8mb4 collate utf8mb4_unicode_ci`,
			database)
		if err := db.Exec(sql).Error; err != nil {
			return err
		}
	}

	return nil
}

func (m *MysqlModule) ApiInitTables(database string, tables ...interface{}) error {
	db, err := m.initConn(&database)
	if err != nil {
		log.SysLogger.Panic(err)
	}

	if err = m.initDatabase(db, database); err != nil {
		return err
	}

	// 配置连接池（参数与主连接保持一致）
	sqlDB, _ := db.DB()
	sqlDB.SetConnMaxIdleTime(time.Minute * 5)
	sqlDB.SetConnMaxLifetime(time.Minute * 10)

	// 设置独立连接池大小
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetMaxOpenConns(2)

	// 执行迁移后立即关闭连接
	defer sqlDB.Close()

	return db.AutoMigrate(tables...)
}

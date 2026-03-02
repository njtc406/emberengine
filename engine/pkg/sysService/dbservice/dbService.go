// Package dbservice
// @Title  数据库服务
// @Description  数据库服务
// @Author  yr  2024/7/25 下午3:10
// @Update  yr  2024/7/25 下午3:10
package dbservice

import (
	"runtime/debug"

	systemConfig "github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/core"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/services"
	mongodbmodule "github.com/njtc406/emberengine/engine/pkg/sysModule/mongomodule"
	"github.com/njtc406/emberengine/engine/pkg/sysModule/mysqlmodule"
	"github.com/njtc406/emberengine/engine/pkg/sysModule/redismodule"
	"github.com/njtc406/emberengine/engine/pkg/sysService/dbservice/config"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"gorm.io/gorm"
)

func RegisterDBService() {
	services.SetService("DBService", func() inf.IService { return &DBService{} })
	systemConfig.RegisterServiceConf(&systemConfig.ServiceConfig{
		ServiceName:   "DBService",
		ConfName:      "db",
		ConfType:      "yaml",
		ConfPath:      "",
		CfgCreator:    func() interface{} { return &config.DBService{} },
		DefaultSetFun: config.DefaultDBService,
		OnChangeFun: func() {
			// TODO 配置变更回调
		},
	})
}

type Callback func(rdb *redis.Client, mdb *gorm.DB, mgdb *mongo.Client, args ...interface{}) (interface{}, error)

type DBService struct {
	core.Service

	redisModule *redismodule.RedisModule
	mysqlModule *mysqlmodule.MysqlModule
	mongoModule *mongodbmodule.MongoModule
}

func (db *DBService) getConf() *config.DBService {
	return db.GetServiceCfg().(*config.DBService)
}

func (db *DBService) OnInit() error {
	conf := db.getConf()
	db.redisModule = redismodule.NewRedisModule()
	db.redisModule.SetLogger(db.GetLogger())
	if err := db.redisModule.Init(conf.RedisConf); err != nil {
		return err
	}
	db.mysqlModule = mysqlmodule.NewMysqlModule()
	db.mysqlModule.SetLogger(db.GetLogger())
	if err := db.mysqlModule.Init(conf.MysqlConf); err != nil {
		return err
	}
	db.mongoModule = mongodbmodule.NewMongoModule()
	db.mongoModule.Init(conf.MongoConf)

	db.AddModule(db.redisModule)
	db.AddModule(db.mysqlModule)
	db.AddModule(db.mongoModule)
	return nil
}

func (db *DBService) OnStart() error {
	db.Infof("db服务启动完成...")
	return nil
}

func (db *DBService) OnRelease() {
	// 释放所有子模块
	db.ReleaseAllChildModule()
	db.Infof("db服务释放完成...")
}

// APIExecuteMixedFun 执行混合函数
func (db *DBService) APIExecuteMixedFun(f Callback, args ...interface{}) (interface{}, error) {
	defer func() {
		if r := recover(); r != nil {
			db.Errorf("mixed execute function panic: %v\ntrace:%s", r, debug.Stack())
		}
	}()
	return f(db.redisModule.GetClient(), db.mysqlModule.GetClient(), db.mongoModule.GetClient(), args...)
}

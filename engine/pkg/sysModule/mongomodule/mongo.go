// Package mongodbmodule
// @Title  mongo模块
// @Description  mongo模块
// @Author  yr  2024/7/25 下午3:12
// @Update  yr  2024/7/25 下午3:12
package mongodbmodule

import (
	"context"
	"github.com/njtc406/emberengine/engine/pkg/core"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"time"
)

type Callback func(ctx context.Context, client *mongo.Client, args ...interface{}) (interface{}, error)

type TransferCallback func(ctx context.Context) (interface{}, error)

type MongoOpt func(ctx context.Context, client *mongo.Client)

var opts []MongoOpt

func Register(fns ...MongoOpt) {
	opts = append(opts, fns...)
}

type Auth struct {
	UserName      string
	Password      string
	AuthMechanism string
	AuthSource    string
}

type MongoConfig struct {
	Uri                string
	MaxOperatorTimeOut time.Duration
	Auth               *Auth
}

type MongoModule struct {
	core.Module
	client             *mongo.Client
	maxOperatorTimeOut time.Duration
}

func NewMongoModule() *MongoModule {
	return &MongoModule{}
}

func (mm *MongoModule) Init(conf *MongoConfig) error {
	var err error
	clientOpts := options.Client().ApplyURI(conf.Uri)
	if conf.Auth != nil {
		clientOpts.SetAuth(options.Credential{
			Username:      conf.Auth.UserName,
			Password:      conf.Auth.Password,
			AuthSource:    conf.Auth.AuthSource,
			AuthMechanism: conf.Auth.AuthMechanism,
		})
	}
	mm.client, err = mongo.Connect(clientOpts)
	if err != nil {
		return err
	}

	ctxTimeout, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = mm.client.Ping(ctxTimeout, nil); err != nil {
		return err
	}

	mm.maxOperatorTimeOut = conf.MaxOperatorTimeOut
	return nil
}

func (mm *MongoModule) OnInit() error {
	for _, opt := range opts {
		opt(context.Background(), mm.client)
	}
	return nil
}

func (mm *MongoModule) Stop() error {
	return mm.client.Disconnect(context.Background())
}

func (mm *MongoModule) GetClient() *mongo.Client {
	return mm.client
}

func (mm *MongoModule) ExecuteFun(f Callback, args ...interface{}) (interface{}, error) {
	ctxTimeout, cancel := context.WithTimeout(context.Background(), mm.maxOperatorTimeOut)
	defer cancel()
	return f(ctxTimeout, mm.client, args...)
}

func (mm *MongoModule) ExecuteTransaction(database string, fun TransferCallback) (interface{}, error) {
	ctxTimeout, cancel := context.WithTimeout(context.Background(), mm.maxOperatorTimeOut)
	defer cancel()

	db := mm.client.Database(database)
	session, err := db.Client().StartSession()
	if err != nil {
		return nil, err
	}
	defer session.EndSession(ctxTimeout)
	result, err := session.WithTransaction(ctxTimeout, fun)
	return result, err
}

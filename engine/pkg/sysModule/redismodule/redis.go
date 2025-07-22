// Package redismodule
// @Title  redis模块
// @Description  redis模块
// @Author  yr  2024/7/25 下午3:13
// @Update  yr  2024/7/25 下午3:13
package redismodule

import (
	"context"
	"encoding/json"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/core"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/redis/go-redis/v9"
)

type RedisModule struct {
	core.Module

	conf   *redis.Options
	client *redis.Client
	// TODO redis需要支持集群模式
	clusterClient *redis.ClusterClient
	tm            *timingwheel.Timer
}

type Callback func(ctx context.Context, tx *redis.Client, args ...interface{}) (interface{}, error)

func NewRedisModule() *RedisModule {
	return &RedisModule{}
}

func (rm *RedisModule) Init(conf *redis.Options) {
	log.SysLogger.Debugf("redis init conf: %+v", conf)
	rm.conf = conf
	rm.client = redis.NewClient(conf)
	if err := rm.checkConnect(); err != nil {
		log.SysLogger.Panic(err)
	}
	rm.tm = rm.TickerAsyncFunc(time.Second*30, "redis health check", func(args ...interface{}) {
		if err := rm.checkConnect(); err != nil {
			rm.reconnect()
		}
	})
}

func (rm *RedisModule) OnRelease() {
	if rm.tm != nil {
		rm.tm.Stop()
	}
	if rm.client != nil {
		_ = rm.client.Close()
	}
}

func (rm *RedisModule) checkConnect() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return rm.client.Ping(ctx).Err()
}

// reconnect 重建连接
func (rm *RedisModule) reconnect() {
	newClient := redis.NewClient(rm.conf)
	if err := newClient.Ping(context.Background()).Err(); err != nil {
		rm.GetLogger().Errorf("Redis reconnect failed: %v", err)
		return
	}

	oldClient := rm.client
	rm.client = newClient // 原子替换

	// 关闭旧连接
	go func() {
		if err := oldClient.Close(); err != nil {
			rm.GetLogger().Errorf("Failed to close old redis client: %v", err)
		}
	}()
}

func (rm *RedisModule) GetClient() *redis.Client {
	return rm.client
}

func (rm *RedisModule) ApiRedisSetString(ctx context.Context, key string, value interface{}, expire time.Duration) error {
	if err := rm.client.Set(ctx, key, value, expire).Err(); err != nil {
		return err
	}
	return nil
}

func (rm *RedisModule) ApiRedisSetStringJson(ctx context.Context, key string, value interface{}, expire time.Duration) error {
	tmp, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err = rm.client.Set(ctx, key, string(tmp), expire).Err(); err != nil {
		return err
	}
	return nil
}

func (rm *RedisModule) ApiRedisGetString(ctx context.Context, key string) (string, error) {
	val, err := rm.client.Get(ctx, key).Result()
	if err != nil {
		return "", err
	}
	return val, nil
}

func (rm *RedisModule) ApiRedisGetStringJson(ctx context.Context, key string, value interface{}) error {
	val, err := rm.client.Get(ctx, key).Result()
	if err != nil {
		return err
	}

	if err = json.Unmarshal([]byte(val), value); err != nil {
		return err
	}
	return nil
}

// ApiRedisHSetStruct 保存结构体(结构体需要带redis标签)
func (rm *RedisModule) ApiRedisHSetStruct(ctx context.Context, key string, value interface{}) error {
	if err := rm.client.HSet(ctx, key, value).Err(); err != nil {
		return err
	}

	return nil
}

// ApiRedisHGetStruct 获取结构体(结构体需要带redis标签)
func (rm *RedisModule) ApiRedisHGetStruct(ctx context.Context, key string, value interface{}) error {
	if err := rm.client.HGetAll(ctx, key).Scan(value); err != nil {
		return err
	}
	return nil
}

// ApiRedisExecuteFun 执行一个函数
func (rm *RedisModule) ApiRedisExecuteFun(ctx context.Context, f Callback, args ...interface{}) (interface{}, error) {
	return f(ctx, rm.client, args...)
}

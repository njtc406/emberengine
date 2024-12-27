// Package remote
// @Title  viper的远程配置
// @Description  用于读取远程配置
// @Author  yr  2024/12/2
// @Update  yr  2024/12/2
package remote

import (
	"bytes"
	"context"
	"fmt"
	"go.uber.org/zap"
	"io"
	"time"

	"github.com/njtc406/viper"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type Config struct {
	viper.RemoteProvider
	client    *clientv3.Client
	closed    chan struct{}
	Endpoints []string
	Username  string
	Password  string
}

func (c *Config) Get(rp viper.RemoteProvider) (io.Reader, error) {
	c.RemoteProvider = rp

	return c.get()
}

func (c *Config) Watch(rp viper.RemoteProvider) (io.Reader, error) {
	// 这接口没啥卵用!!
	c.RemoteProvider = rp

	return c.get()
}

func (c *Config) WatchChannel(rp viper.RemoteProvider) (<-chan *viper.RemoteResponse, chan bool) {
	c.RemoteProvider = rp

	rr := make(chan *viper.RemoteResponse)
	stop := make(chan bool)
	ch := c.client.Watch(context.Background(), c.RemoteProvider.Path())

	go func() {
		for {
			select {
			case <-c.closed:
				close(stop) // 系统主动关闭
			case <-stop:
				if len(ch) > 0 {
					// 检查是否处理完成
					for i := 0; i < len(ch); i++ {
						// 将所有事件都处理完
						res := <-ch
						for _, event := range res.Events {
							rr <- &viper.RemoteResponse{
								Value: event.Kv.Value,
							}
						}
					}
				}
				// 通知处理器关闭
				close(rr)
				return
			case res := <-ch:
				for _, event := range res.Events {
					ev := &viper.RemoteResponse{
						Value: event.Kv.Value,
					}
					select {
					case rr <- ev:
					default:
						_, ok := <-rr
						if !ok {
							// 外层的监听已经关闭,退出当前监听
							return
						}
						fmt.Println("remote watch channel full")
					}
				}
			}
		}
	}()

	return rr, stop
}

func (c *Config) newClient() (*clientv3.Client, error) {
	//logger, err := zap.NewDevelopment()
	//if err != nil {
	//	return nil, err
	//}
	client, err := clientv3.New(clientv3.Config{
		Endpoints: c.Endpoints,
		Username:  c.Username,
		Password:  c.Password,
		Logger:    zap.NewNop(),
	})

	if err != nil {
		return nil, err
	}

	return client, nil
}

func (c *Config) get() (io.Reader, error) {
	if c.client == nil {
		client, err := c.newClient()

		if err != nil {
			return nil, err
		}

		c.client = client
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	resp, err := c.client.Get(ctx, c.Path())
	defer cancel()

	if err != nil {
		return nil, err
	}

	if len(resp.Kvs) == 0 {
		return bytes.NewReader([]byte{}), nil
	}

	return bytes.NewReader(resp.Kvs[0].Value), nil
}

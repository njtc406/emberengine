// Package asynclib
// Mode ServiceName: 异步执行
// Mode Desc: 使用协程池中的协程执行任务,防止出现瞬间创建大量协程,出现性能问题
package asynclib

import (
	"fmt"
	"github.com/panjf2000/ants/v2"
)

// TODO 协程池放入service中，全局的这个只初始化一个小一点的池子,每个service根据自己的设置初始化对应数量的协程池

// antsPool 协程池
var antsPool *ants.Pool

func InitAntsPool(size int) {
	if antsPool == nil {
		antsPool = NewAntsPool(size, ants.WithPreAlloc(true))
	}
}

// NewAntsPool 创建协程池
// size表示池子的大小
func NewAntsPool(size int, options ...ants.Option) *ants.Pool {
	p, err := ants.NewPool(size, options...)
	if err != nil {
		panic(err)
	}
	return p
}

func Go(f func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("groutine exec func failed, err:%v", r)
		}
	}()

	return antsPool.Submit(f)
}

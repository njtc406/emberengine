// Package asynclib
// Mode ServiceName: 异步执行
// Mode Desc: 使用协程池中的协程执行任务,防止出现瞬间创建大量协程,出现性能问题
package asynclib

import (
	"fmt"
	"github.com/panjf2000/ants/v2"
)

// antsPool 协程池
var antsPool *ants.Pool

func InitAntsPool(size int) {
	if antsPool == nil {
		var err error
		antsPool, err = ants.NewPool(size, ants.WithPreAlloc(true))
		if err != nil {
			panic(err)
		}
	}
}

func Go(f func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("groutine exec func failed, err:%v", r)
		}
	}()

	return antsPool.Submit(f)
}

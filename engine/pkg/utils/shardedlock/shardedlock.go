// Package shardedlock
// @Title  title
// @Description  desc
// @Author  yr  2025/4/11
// @Update  yr  2025/4/11
package shardedlock

import (
	"fmt"
	"hash/fnv"
	"sync"
)

type ShardedRWLock struct {
	shards []sync.RWMutex
	mask   uint32
}

func NewShardedRWLock(shardCount int) *ShardedRWLock {
	if shardCount <= 0 || (shardCount&(shardCount-1)) != 0 {
		panic("shardCount must be power of 2")
	}

	return &ShardedRWLock{
		shards: make([]sync.RWMutex, shardCount),
		mask:   uint32(shardCount - 1),
	}
}

// 泛型哈希函数，支持任意类型
func hash(v any) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(fmt.Sprintf("%v", v)))
	return h.Sum32()
}

func (s *ShardedRWLock) get(key any) *sync.RWMutex {
	idx := hash(key) & s.mask
	return &s.shards[idx]
}

func (s *ShardedRWLock) RLock(key any) {
	s.get(key).RLock()
}

func (s *ShardedRWLock) RUnlock(key any) {
	s.get(key).RUnlock()
}

func (s *ShardedRWLock) Lock(key any) {
	s.get(key).Lock()
}

func (s *ShardedRWLock) Unlock(key any) {
	s.get(key).Unlock()
}

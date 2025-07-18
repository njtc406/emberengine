// Package pool
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/18 0018 0:10
// 最后更新:  yr  2025/7/18 0018 0:10
package pool

import (
	"encoding/json"
	"sync/atomic"
)

// IStatsRecorder 统计
type IStatsRecorder interface {
	incHit()
	incMiss()
	incCurrentSize()
	decCurrentSize()
	incOverflow()
	incTotalAlloc()
	stats() Stats
}

type nopStats struct{}

func NewNoStatsRecorder() IStatsRecorder {
	return nopStats{}
}

func (nopStats) incHit()         {}
func (nopStats) incMiss()        {}
func (nopStats) incCurrentSize() {}
func (nopStats) decCurrentSize() {}
func (nopStats) incOverflow()    {}
func (nopStats) incTotalAlloc()  {}
func (nopStats) stats() Stats {
	return Stats{}
}

type Stats struct {
	Name string

	_ [cacheLineSize]byte

	HitCount        int64 // 命中本地池
	_               [cacheLineSize - 8]byte
	MissCount       int64 // 未命中本地池（从全局取）
	_               [cacheLineSize - 8]byte
	OverflowCount   int64 // 本地池满了，放入全局
	_               [cacheLineSize - 8]byte
	CurrentSize     int64 // 当前对象数（本地,不是准确数量）(数量如果和总分配数对不上,表明全局池缓存了对象,缓存的命中率降低了)
	_               [cacheLineSize - 8]byte
	TotalAlloc      int64 // 总共分配的新对象数
	_               [cacheLineSize - 8]byte
	MaxObservedSize int64 // 最大对象数(本地)
}

func (s *Stats) String() string {
	data, err := json.Marshal(s)
	if err != nil {
		return ""
	}
	return string(data)
}

func NewStatsRecorder(name string) IStatsRecorder {
	return &Stats{Name: name}
}
func (s *Stats) incHit() {
	atomic.AddInt64(&s.HitCount, 1)
}
func (s *Stats) incMiss() {
	atomic.AddInt64(&s.MissCount, 1)
}
func (s *Stats) incCurrentSize() {
	newSize := atomic.AddInt64(&s.CurrentSize, 1)

	// 循环更新MaxObservedSize
	for {
		currentMax := atomic.LoadInt64(&s.MaxObservedSize)
		// 如果新值不超过当前最大值，则退出
		if newSize <= currentMax {
			break
		}
		// 尝试原子更新最大值
		if atomic.CompareAndSwapInt64(&s.MaxObservedSize, currentMax, newSize) {
			break
		}
		// 其他goroutine已更新，重试
	}
}

func (s *Stats) decCurrentSize() {
	atomic.AddInt64(&s.CurrentSize, -1)
}

func (s *Stats) incTotalAlloc() {
	atomic.AddInt64(&s.TotalAlloc, 1)
}

func (s *Stats) incOverflow() {
	atomic.AddInt64(&s.OverflowCount, 1)
}

func (s *Stats) stats() Stats {
	return *s
}

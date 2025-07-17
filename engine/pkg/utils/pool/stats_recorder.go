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

	HitCount      int64 // 命中本地池
	MissCount     int64 // 未命中本地池（从全局取）
	OverflowCount int64 // 本地池满了，放入全局
	CurrentSize   int64 // 当前对象数（含本地和全局，估算）
	TotalAlloc    int64 // 总共分配的新对象数
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
	atomic.AddInt64(&s.CurrentSize, 1)
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

package monitor

import (
	"sync/atomic"
	"testing"
	"time"
)

// 当前实现
func BenchmarkGenSeq_Current(b *testing.B) {
	var epoch uint64 = uint64(time.Now().UnixNano()&0xFFFFF) << 44
	var seq uint64
	const seqMask = uint64(0xFFFFFFFFFFF)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			s := atomic.AddUint64(&seq, 1) & seqMask
			if s == 0 {
				atomic.StoreUint64(&epoch, uint64(time.Now().UnixNano()&0xFFFFF)<<44)
			}
			_ = atomic.LoadUint64(&epoch) | s
		}
	})
}

// 极致性能版本（非原子读epoch）
func BenchmarkGenSeq_Fast(b *testing.B) {
	var epoch uint64 = uint64(time.Now().UnixNano()&0xFFFFF) << 44
	var seq uint64
	const seqMask = uint64(0xFFFFFFFFFFF)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			s := atomic.AddUint64(&seq, 1) & seqMask
			if s == 0 {
				epoch = uint64(time.Now().UnixNano()&0xFFFFF) << 44
			}
			_ = epoch | s
		}
	})
}

// 原始纯自增版本（对照组）
func BenchmarkGenSeq_Original(b *testing.B) {
	var seed uint64

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = atomic.AddUint64(&seed, 1)
		}
	})
}

// 单线程测试
func BenchmarkGenSeq_Current_SingleThread(b *testing.B) {
	var epoch uint64 = uint64(time.Now().UnixNano()&0xFFFFF) << 44
	var seq uint64
	const seqMask = uint64(0xFFFFFFFFFFF)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := atomic.AddUint64(&seq, 1) & seqMask
		if s == 0 {
			atomic.StoreUint64(&epoch, uint64(time.Now().UnixNano()&0xFFFFF)<<44)
		}
		_ = atomic.LoadUint64(&epoch) | s
	}
}

func BenchmarkGenSeq_Original_SingleThread(b *testing.B) {
	var seed uint64

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = atomic.AddUint64(&seed, 1)
	}
}

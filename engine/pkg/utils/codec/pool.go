// Package serializer
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/19 0019 13:47
// 最后更新:  yr  2025/7/19 0019 13:47
package codec

import (
	"fmt"

	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
)

const Kib = 1024

var runtimeDebug bool

func SetDebug(enabled bool) {
	runtimeDebug = enabled
}

type SerializedData struct {
	buf []byte
}

func (sd *SerializedData) Reset() {
	sd.buf = sd.buf[:0]
}

func (sd *SerializedData) Release() {
	if sd.buf != nil {
		bytePoolMgr.GetPool(cap(sd.buf)).Put(sd)
	}
}

func (sd *SerializedData) Get() []byte {
	return sd.buf
}

func (sd *SerializedData) GetBytes() []byte {
	return sd.buf[:len(sd.buf)]
}

// 多规格 Kib 缓冲池（你可以根据业务实际调整大小）
var bytePoolMgr = NewBytePoolManager([]int{
	2 * Kib, 8 * Kib, 16 * Kib, 32 * Kib, 64 * Kib, 128 * Kib, 512 * Kib, 1024 * Kib, 2048 * Kib,
})

type BytePoolManager struct {
	pools []pool.IPool[*SerializedData]
	sizes []int // 阈值上限：升序排序
}

func NewBytePoolManager(sizes []int) *BytePoolManager {
	pools := make([]pool.IPool[*SerializedData], len(sizes))
	for i, sz := range sizes {
		recorder := pool.NewNoStatsRecorder()
		if runtimeDebug {
			recorder = pool.NewStatsRecorder(fmt.Sprintf("bytePool_%dKB", sz/1024))
		}
		pools[i] = pool.NewSyncPoolWrapper(
			func() *SerializedData { return &SerializedData{buf: make([]byte, 0, sz)} },
			recorder,
			pool.WithReset(func(b *SerializedData) { b.Reset() }),
		)
	}
	return &BytePoolManager{
		pools: pools,
		sizes: sizes,
	}
}

// GetPool returns the most suitable pool for a given size.
func (m *BytePoolManager) GetPool(size int) pool.IPool[*SerializedData] {
	for i := 0; i < len(m.sizes); i++ {
		if size <= m.sizes[i] {
			return m.pools[i]
		}
	}
	// 如果超出最大规格，使用最大那个池（或者 panic）
	return m.pools[len(m.pools)-1]
}

func (m *BytePoolManager) Stats() []*pool.Stats {
	stats := make([]*pool.Stats, len(m.pools))
	for i, p := range m.pools {
		stats[i] = p.Stats()
	}
	return stats
}

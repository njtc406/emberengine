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

// 多规格 Byte 缓冲池（你可以根据业务实际调整大小）
var bytePoolMgr = NewBytePoolManager([]int{
	128 * 1024, 512 * 1024, 1024 * 1024, 2048 * 1024,
})

type BytePoolManager struct {
	pools []pool.IPool[*SerializedData]
	sizes []int // 阈值上限：升序排序
}

func NewBytePoolManager(sizes []int) *BytePoolManager {
	pools := make([]pool.IPool[*SerializedData], len(sizes))
	for i, sz := range sizes {
		pools[i] = pool.NewSyncPoolWrapper(
			func() *SerializedData { return &SerializedData{buf: make([]byte, 0, sz)} },
			pool.NewStatsRecorder(fmt.Sprintf("bytePool_%dKB", sz/1024)),
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
	for i, sz := range m.sizes {
		if size <= sz {
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

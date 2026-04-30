// Package mailbox
// @Title  Dispatch 调度环
// @Description  基于 xxhash + Lamping & Veach jump consistent hash 的轻量调度环，将 dispatcherKey 映射到当前活跃 Worker，构建复杂度 O(N log N)，无虚拟节点。
// @Author  yr  2026/4/27
package mailbox

import (
	"sort"

	"github.com/cespare/xxhash/v2"
)

// dispatchRing 用于将 dispatcherKey 映射到 active worker ID，基于 jump
// consistent hash，不需要虚拟节点：
//
//   - Build 成本：对 active worker ID 做一次排序；
//   - 内存：O(N)；
//   - Get 成本：xxhash64(key) + jumpHash(O(log N)) + 切片下标。
//
// 一致性 / 拓扑变更成本说明：
//   - 稳态（worker 数不变）下，相同 dispatcherKey 始终命中相同 worker——与
//     consistent hash 一致。
//   - 拓扑变更时，jump consistent hash 会重新分布相当一部分 key 到新的 bucket
//     （传统 consistent hash 的 key 变更比例约为 K/N）。但本框架的扩缩容契约保证：
//     缩容前老 worker 已 BeginStop+Wait drain 完毕；新 snapshot 发布后才
//     接收 rehash 后的新 Job，老 dispatcherKey 的 in-flight 已结束，
//     因此拓扑变更不会破坏"同 key 顺序"语义。
//
// 调度环构建后只读，安全在多 goroutine 并发 Get。
type dispatchRing struct {
	// sortedIDs 是排序后的 active worker ID 切片。下标即 jump hash bucket。
	// 排序保证 build 输入相同 → 输出相同（利于 hot path 缓存友好）。
	sortedIDs []int32
}

// newDispatchRing 用 active worker IDs 构建一个新的调度环。
// 输入切片可能是无序的，本函数会拷贝并排序，不修改入参。
func newDispatchRing(ids []int32) *dispatchRing {
	r := &dispatchRing{sortedIDs: append([]int32(nil), ids...)}
	sort.Slice(r.sortedIDs, func(i, j int) bool { return r.sortedIDs[i] < r.sortedIDs[j] })
	return r
}

// Get 把 dispatcherKey 路由到一个 active worker ID。
// 空环返回 (0, false)；其他情况返回 (id, true)。
func (r *dispatchRing) Get(key string) (int32, bool) {
	if r == nil || len(r.sortedIDs) == 0 {
		return 0, false
	}
	h := xxhash.Sum64String(key)
	idx := jumpHash(h, int32(len(r.sortedIDs)))
	return r.sortedIDs[idx], true
}

// Len 返回环中 worker 数。
func (r *dispatchRing) Len() int {
	if r == nil {
		return 0
	}
	return len(r.sortedIDs)
}

// jumpHash 实现 Lamping & Veach (2014) "A Fast, Minimal Memory, Consistent Hash
// Algorithm" —— 把 64 位 key 映射到 [0, n) 区间的 bucket。
//
// 性质：
//   - 严格均匀分布（无需虚拟节点）；
//   - O(log n) 时间，常数因子极小，无内存分配；
//   - 当 n 由 N → N+1 时，恰好 1/(N+1) 比例的 key 重新映射；
//     当 n 由 N → N-1 时，恰好 1/N 比例的 key 重新映射。
//
// 对 n <= 0 的情况保护性地返回 0（调用方在 Get 中已先检查 len==0）。
func jumpHash(key uint64, n int32) int32 {
	if n <= 0 {
		return 0
	}
	var b, j int64 = -1, 0
	for j < int64(n) {
		b = j
		key = key*2862933555777941757 + 1
		// 注意：key>>33 后落在 [0, 2^31)，加 1 防止除零。
		j = int64(float64(b+1) * (float64(int64(1)<<31) / float64((key>>33)+1)))
	}
	return int32(b)
}

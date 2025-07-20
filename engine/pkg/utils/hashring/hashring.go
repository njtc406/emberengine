// Package hashring
// @Title  哈希环
// @Description  desc
// @Author  yr  2025/4/18
// @Update  yr  2025/4/18
package hashring

import (
	"fmt"
	"github.com/cespare/xxhash/v2"
	"sort"
	"sync"
)

const hashSalt = "ember_salt_key_y_w"

//func hashEvent(key string) int {
//	// key为空时,给固定的1
//	if key == "" {
//		return 1
//	}
//	// 使用 FNV-1a 哈希算法
//	h := fnv.New32a()
//	_, _ = h.Write([]byte(key))
//	return int(h.Sum32())
//}

func hashEvent(key string) int {
	if key == "" {
		return 1
	}
	return int(xxhash.Sum64String(key)) // 用 64-bit 更好分布
}

// HashRing 表示一个带虚拟节点的一致性哈希环。
type HashRing[T comparable] struct {
	nodes    []int        // 排序后的虚拟节点哈希值
	ring     map[int]T    // 虚拟节点哈希值
	replicas int          // 每个节点的虚拟节点数量
	mu       sync.RWMutex // 保护 nodes 和 ring
}

func NewHashRing[T comparable](replicas int) *HashRing[T] {
	return &HashRing[T]{
		nodes:    []int{},
		ring:     make(map[int]T),
		replicas: replicas,
	}
}

func (h *HashRing[T]) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nodes = nil
	h.ring = nil
	h.replicas = 0
}

// Add 添加到哈希环中，并生成对应的虚拟节点。
func (h *HashRing[T]) Add(key T) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := 0; i < h.replicas; i++ {
		virtualNodeKey := fmt.Sprintf("%s-%d-%d", hashSalt, key, i)
		hash := hashEvent(virtualNodeKey)
		h.nodes = append(h.nodes, hash)
		h.ring[hash] = key
	}
	sort.Ints(h.nodes)
}

// Remove 从哈希环中移除，其对应的所有虚拟节点都会被删除。
func (h *HashRing[T]) Remove(key T) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var newNodes []int
	for _, hash := range h.nodes {
		if h.ring[hash] == key {
			delete(h.ring, hash)
		} else {
			newNodes = append(newNodes, hash)
		}
	}
	h.nodes = newNodes
}

// Get 根据传入的 key 计算哈希值，并在哈希环中查找对应的值。
func (h *HashRing[T]) Get(hashKey string) (T, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var zero T

	if len(h.nodes) == 0 {
		return zero, false
	}
	hash := hashEvent(hashKey)
	// 二分查找第一个 >= hash 的虚拟节点
	idx := sort.Search(len(h.nodes), func(i int) bool {
		return h.nodes[i] >= hash
	})
	if idx == len(h.nodes) {
		idx = 0
	}
	key, ok := h.ring[h.nodes[idx]]

	return key, ok
}

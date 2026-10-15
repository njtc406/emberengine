// Package mapx
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/24 0024 0:10
// 最后更新:  yr  2025/7/24 0024 0:10
package mapx

type Map[K comparable, V any] map[K]V

func NewMapX[K comparable, V any]() Map[K, V] {
	return make(map[K]V)
}

func (m Map[K, V]) Get(key K) (V, bool) {
	val, ok := m[key]
	return val, ok
}

func (m Map[K, V]) Set(key K, val V) {
	m[key] = val
}

func (m Map[K, V]) Delete(key K) {
	delete(m, key)
}

func (m Map[K, V]) Clear() {
	for k := range m {
		delete(m, k)
	}
}

func (m Map[K, V]) Len() int {
	return len(m)
}

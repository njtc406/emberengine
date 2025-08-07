// Package syncx
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/8/8 0008 1:05
// 最后更新:  yr  2025/8/8 0008 1:05
package syncx

import "sync"

type Map[K comparable, V any] struct {
	m    sync.Map
	zero func() V
}

func NewMap[K comparable, V any]() *Map[K, V] {
	return &Map[K, V]{
		zero: func() V {
			var v V
			return v
		},
	}
}

func (m *Map[K, V]) Load(key K) (value V, ok bool) {
	v, ok := m.m.Load(key)
	if !ok {
		return m.zero(), false
	}
	return v.(V), true
}

func (m *Map[K, V]) Store(key K, value V) {
	m.m.Store(key, value)
}

func (m *Map[K, V]) Delete(key K) {
	m.m.Delete(key)
}

func (m *Map[K, V]) Range(f func(key K, value V) bool) {
	m.m.Range(func(key, value any) bool {
		return f(key.(K), value.(V))
	})
}

func (m *Map[K, V]) LoadOrStore(key K, value V) (actual V, loaded bool) {
	v, loaded := m.m.LoadOrStore(key, value)
	if !loaded {
		return m.zero(), false
	}
	return v.(V), loaded
}

func (m *Map[K, V]) LoadAndDelete(key K) (value V, loaded bool) {
	v, loaded := m.m.LoadAndDelete(key)
	if !loaded {
		return m.zero(), false
	}
	return v.(V), loaded
}

func (m *Map[K, V]) Swap(key K, value V) (previous V, loaded bool) {
	v, loaded := m.m.Swap(key, value)
	if !loaded {
		return m.zero(), false
	}
	return v.(V), loaded
}

func (m *Map[K, V]) CompareAndSwap(key K, old, new V) (swapped bool) {
	return m.m.CompareAndSwap(key, old, new)
}

func (m *Map[K, V]) CompareAndDelete(key K, old V) (deleted bool) {
	return m.m.CompareAndDelete(key, old)
}

func (m *Map[K, V]) Clear() {
	m.m.Clear()
}

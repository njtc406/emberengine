// pool_test.go
package pool

import (
	"github.com/njtc406/emberengine/engine/pkg/dto"
	"testing"
)

type TestData struct {
	dto.DataRef
	Data   int
	Buffer []byte
}

func (t *TestData) Reset() {
	t.Data = 0
	//t.Buffer = t.Buffer[:0]
}

// 基准测试
func BenchmarkPool(b *testing.B) {
	p := NewPool(1024, func() *TestData {
		return &TestData{}
	})

	for i := 0; i < b.N; i++ {
		data := p.Get()
		data.Data = i
		p.Put(data)
	}
}

/*
goos: windows
goarch: amd64
pkg: github.com/njtc406/emberengine/engine/pkg/utils/pool
cpu: AMD Ryzen 7 2700 Eight-Core Processor
BenchmarkPool
BenchmarkPool-16        32350593                36.85 ns/op
PASS
*/

func BenchmarkPoolParallel(b *testing.B) {
	p := NewPool(1024, func() *TestData {
		return &TestData{}
	})
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			obj := p.Get()
			p.Put(obj)
		}
	})
}

/*
goos: windows
goarch: amd64
pkg: github.com/njtc406/emberengine/engine/pkg/utils/pool
cpu: AMD Ryzen 7 2700 Eight-Core Processor
BenchmarkPoolParallel
BenchmarkPoolParallel-16        18488616                61.95 ns/op
PASS
*/

func BenchmarkExtendedPool(b *testing.B) {
	p := NewExtendedPool(1024, func() *TestData {
		return &TestData{}
	})
	for i := 0; i < b.N; i++ {
		data := p.Get()
		data.Data = i
		p.Put(data)
	}
}

/*
goos: windows
goarch: amd64
pkg: github.com/njtc406/emberengine/engine/pkg/utils/pool
cpu: AMD Ryzen 7 2700 Eight-Core Processor
BenchmarkExtendedPool
BenchmarkExtendedPool-16        19716768                62.17 ns/op
PASS
*/

func BenchmarkExtendedPoolParallel(b *testing.B) {
	p := NewExtendedPool(1024, func() *TestData {
		return &TestData{}
	})
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			obj := p.Get()
			p.Put(obj)
		}
	})
}

func BenchmarkChannelPool_GetPut(b *testing.B) {
	pool := NewChannelPool[*TestData](1024, func() *TestData {
		return &TestData{}
	})
	for i := 0; i < b.N; i++ {
		obj := pool.Get()
		pool.Put(obj)
	}
}

/*
goos: windows
goarch: amd64
pkg: github.com/njtc406/emberengine/engine/pkg/utils/pool
cpu: AMD Ryzen 7 2700 Eight-Core Processor
BenchmarkChannelPool_GetPut
BenchmarkChannelPool_GetPut-16          19964828                58.48 ns/op
PASS
*/

func BenchmarkChannelPool_GetPutParallel(b *testing.B) {
	pool := NewChannelPool[*TestData](1024, func() *TestData {
		return &TestData{}
	})
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			obj := pool.Get()
			pool.Put(obj)
		}
	})
}

/*
goos: windows
goarch: amd64
pkg: github.com/njtc406/emberengine/engine/pkg/utils/pool
cpu: AMD Ryzen 7 2700 Eight-Core Processor
BenchmarkChannelPool_GetPutParallel
BenchmarkChannelPool_GetPutParallel-16           1337701               879.5 ns/
op
PASS
*/

/*
goos: windows
goarch: amd64
pkg: github.com/njtc406/emberengine/engine/pkg/utils/pool
cpu: AMD Ryzen 7 2700 Eight-Core Processor
BenchmarkExtendedPoolParallel
BenchmarkExtendedPoolParallel-16        14852881                79.54 ns/op
PASS
*/

func BenchmarkPrePPool_DifferentSizes(b *testing.B) {
	sizes := []struct {
		name string
		size int
	}{
		{"Small", 16},
		{"Medium", 128},
		{"Large", 1024},
	}

	for _, sz := range sizes {
		b.Run(sz.name, func(b *testing.B) {
			p := NewPrePPool(1024, func() *TestData {
				return &TestData{
					Buffer: make([]byte, sz.size, sz.size),
				}
			})

			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					obj := p.Get()
					obj.Buffer[0] = 4
					p.Put(obj)
				}
			})
		})
	}

}

/*
goos: windows
goarch: amd64
pkg: github.com/njtc406/emberengine/engine/pkg/utils/pool
cpu: AMD Ryzen 7 2700 Eight-Core Processor
BenchmarkPrePPool_DifferentSizes
BenchmarkPrePPool_DifferentSizes/Small
BenchmarkPrePPool_DifferentSizes/Small-16               677140525 1.698 ns/op           0 B/op          0 allocs/op
BenchmarkPrePPool_DifferentSizes/Medium
BenchmarkPrePPool_DifferentSizes/Medium-16              716748255 1.657 ns/op           0 B/op          0 allocs/op
BenchmarkPrePPool_DifferentSizes/Large
BenchmarkPrePPool_DifferentSizes/Large-16               735854124 1.645 ns/op           0 B/op          0 allocs/op
PASS

*/

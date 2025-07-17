// pool_test.go
package pool

import (
	"github.com/njtc406/emberengine/engine/pkg/dto"
	"sync"
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

goos: windows
goarch: amd64
pkg: github.com/njtc406/emberengine/engine/pkg/utils/pool
cpu: AMD Ryzen 7 5700X 8-Core Processor
BenchmarkPool
BenchmarkPool-16        100000000               12.41 ns/op
PASS
*/

func BenchmarkPoolParallel(b *testing.B) {
	p := NewPool(1024, func() *TestData {
		return &TestData{}
	})
	b.ReportAllocs()
	b.ResetTimer()
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

goos: windows
goarch: amd64
pkg: github.com/njtc406/emberengine/engine/pkg/utils/pool
cpu: AMD Ryzen 7 5700X 8-Core Processor
BenchmarkPoolParallel
BenchmarkPoolParallel-16        58341921                21.55 ns/op            0 B/op          0 allocs/op
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

goos: windows
goarch: amd64
pkg: github.com/njtc406/emberengine/engine/pkg/utils/pool
cpu: AMD Ryzen 7 5700X 8-Core Processor
BenchmarkExtendedPool
BenchmarkExtendedPool-16        67712065                18.56 ns/op
PASS
*/

func BenchmarkExtendedPoolParallel(b *testing.B) {
	p := NewExtendedPool(1024, func() *TestData {
		return &TestData{}
	})
	b.ReportAllocs()
	b.ResetTimer()
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
BenchmarkExtendedPoolParallel
BenchmarkExtendedPoolParallel-16        14852881                79.54 ns/op
PASS

goos: windows
goarch: amd64
pkg: github.com/njtc406/emberengine/engine/pkg/utils/pool
cpu: AMD Ryzen 7 5700X 8-Core Processor
BenchmarkExtendedPoolParallel
BenchmarkExtendedPoolParallel-16        41417299                34.31 ns/op     0 B/op          0 allocs/op
PASS
*/

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

goos: windows
goarch: amd64
pkg: github.com/njtc406/emberengine/engine/pkg/utils/pool
cpu: AMD Ryzen 7 5700X 8-Core Processor
BenchmarkChannelPool_GetPut
BenchmarkChannelPool_GetPut-16          28604186                38.80 ns/op
PASS
*/

func BenchmarkChannelPool_GetPutParallel(b *testing.B) {
	pool := NewChannelPool[*TestData](1024, func() *TestData {
		return &TestData{}
	})
	b.ReportAllocs()
	b.ResetTimer()
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

goos: windows
goarch: amd64
pkg: github.com/njtc406/emberengine/engine/pkg/utils/pool
cpu: AMD Ryzen 7 5700X 8-Core Processor
BenchmarkChannelPool_GetPutParallel
BenchmarkChannelPool_GetPutParallel-16           6131739               195.5 ns/op             0 B/op          0 allocs/op
PASS
*/

func BenchmarkSyncPool(b *testing.B) {
	pool := sync.Pool{
		New: func() interface{} {
			return &TestData{}
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data := pool.Get()
		pool.Put(data)
	}
}

/*
goos: windows
goarch: amd64
pkg: github.com/njtc406/emberengine/engine/pkg/utils/pool
cpu: AMD Ryzen 7 5700X 8-Core Processor
BenchmarkSyncPool
BenchmarkSyncPool-16            100000000               11.14 ns/op
PASS
*/

func BenchmarkSyncPool_GetPutParallel(b *testing.B) {
	pool := NewSyncPool(func() *TestData {
		return &TestData{}
	})
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			data := pool.Get()
			pool.Put(data)
		}
	})
}

/*
goos: windows
goarch: amd64
pkg: github.com/njtc406/emberengine/engine/pkg/utils/pool
cpu: AMD Ryzen 7 5700X 8-Core Processor
BenchmarkSyncPool_GetPutParallel
BenchmarkSyncPool_GetPutParallel-16     800038401                1.442 ns/op             0 B/op          0 allocs/op
PASS

goos: windows
goarch: amd64
pkg: github.com/njtc406/emberengine/engine/pkg/utils/pool
cpu: AMD Ryzen 7 5700X 8-Core Processor
BenchmarkSyncPool_GetPutParallel
BenchmarkSyncPool_GetPutParallel-16     579820587                2.025 ns/op           0 B/op          0 allocs/op
PASS
*/

func BenchmarkSyncPool__DifferentSizes(b *testing.B) {
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
			p := NewSyncPool(func() *TestData {
				return &TestData{Buffer: make([]byte, sz.size)}
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
cpu: AMD Ryzen 7 5700X 8-Core Processor
BenchmarkSyncPool__DifferentSizes
BenchmarkSyncPool__DifferentSizes/Small
BenchmarkSyncPool__DifferentSizes/Small-16              728524464 1.549 ns/op           0 B/op          0 allocs/op
BenchmarkSyncPool__DifferentSizes/Medium
BenchmarkSyncPool__DifferentSizes/Medium-16             766894198 1.514 ns/op           0 B/op          0 allocs/op
BenchmarkSyncPool__DifferentSizes/Large
BenchmarkSyncPool__DifferentSizes/Large-16              804266904 1.554 ns/op           0 B/op          0 allocs/op
PASS

goos: windows
goarch: amd64
pkg: github.com/njtc406/emberengine/engine/pkg/utils/pool
cpu: AMD Ryzen 7 5700X 8-Core Processor
BenchmarkSyncPool__DifferentSizes
BenchmarkSyncPool__DifferentSizes/Small
BenchmarkSyncPool__DifferentSizes/Small-16              547810628 3.428 ns/op           0 B/op          0 allocs/op
BenchmarkSyncPool__DifferentSizes/Medium
BenchmarkSyncPool__DifferentSizes/Medium-16             564736975 2.052 ns/op           0 B/op          0 allocs/op
BenchmarkSyncPool__DifferentSizes/Large
BenchmarkSyncPool__DifferentSizes/Large-16              580636310 2.077 ns/op           0 B/op          0 allocs/op
PASS
*/

func BenchmarkPrePPool_GetPut(b *testing.B) {
	pool := NewPrePPool[*TestData](1024, func() *TestData {
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
BenchmarkPrePPool_GetPut
BenchmarkPrePPool_GetPut-16     93102645                13.11 ns/op
PASS

goos: windows
goarch: amd64
pkg: github.com/njtc406/emberengine/engine/pkg/utils/pool
cpu: AMD Ryzen 7 5700X 8-Core Processor
BenchmarkPrePPool_GetPut
BenchmarkPrePPool_GetPut-16     81989054                13.69 ns/op
PASS
*/

func BenchmarkPrePPool_GetPutParallel(b *testing.B) {
	pool := NewPrePPool[*TestData](1024, func() *TestData {
		return &TestData{}
	})
	b.ReportAllocs()
	b.ResetTimer()
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
BenchmarkPrePPool_GetPutParallel
BenchmarkPrePPool_GetPutParallel-16     706656765                1.552 ns/op
PASS

goos: windows
goarch: amd64
pkg: github.com/njtc406/emberengine/engine/pkg/utils/pool
cpu: AMD Ryzen 7 5700X 8-Core Processor
BenchmarkPrePPool_GetPutParallel
BenchmarkPrePPool_GetPutParallel-16     830596977                1.425 ns/op
               0 B/op          0 allocs/op
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

goos: windows
goarch: amd64
pkg: github.com/njtc406/emberengine/engine/pkg/utils/pool
cpu: AMD Ryzen 7 5700X 8-Core Processor
BenchmarkPrePPool_DifferentSizes
BenchmarkPrePPool_DifferentSizes/Small
BenchmarkPrePPool_DifferentSizes/Small-16               759898147 1.848 ns/op           0 B/op          0 allocs/op
BenchmarkPrePPool_DifferentSizes/Medium
BenchmarkPrePPool_DifferentSizes/Medium-16              737646268 1.801 ns/op           0 B/op          0 allocs/op
BenchmarkPrePPool_DifferentSizes/Large
BenchmarkPrePPool_DifferentSizes/Large-16               747557821 1.687 ns/op           0 B/op          0 allocs/op
PASS
*/

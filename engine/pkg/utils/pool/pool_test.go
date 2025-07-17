// Package pool
// @Title  title
// @Description  desc
// @Author  yr  2025/7/17
// @Update  yr  2025/7/17
package pool

import "testing"

type TestData struct {
	A      int
	Buffer []byte
}

func BenchmarkSyncPoolWrapper_GetPut(b *testing.B) {
	//pool := NewSyncPoolWrapper(func() *TestData {
	//	return &TestData{}
	//}, NewStatsRecorder("test"))
	pool := NewSyncPoolWrapper(func() *TestData {
		return &TestData{}
	}, nil)
	for i := 0; i < b.N; i++ {
		pool.Put(pool.Get())
	}
	b.Logf("stats: %+v", pool.Stats())
}

/*
goos: windows
goarch: amd64
pkg: github.com/njtc406/emberengine/engine/pkg/utils/pool
cpu: AMD Ryzen 7 5700X 8-Core Processor
BenchmarkSyncPoolWrapper_GetPut
    pool_test.go:17: stats: {Name:test CurrentSize:1 HitCount:1 MissCount:0 T
otalAlloc:0}
    pool_test.go:17: stats: {Name:test CurrentSize:100 HitCount:100 MissCount
:0 TotalAlloc:0}
    pool_test.go:17: stats: {Name:test CurrentSize:10000 HitCount:10000 MissC
ount:0 TotalAlloc:0}
    pool_test.go:17: stats: {Name:test CurrentSize:1000000 HitCount:1000000 M
issCount:0 TotalAlloc:0}
    pool_test.go:17: stats: {Name:test CurrentSize:80901238 HitCount:80901238
 MissCount:0 TotalAlloc:0}
BenchmarkSyncPoolWrapper_GetPut-16      80901238                15.07 ns/op
PASS
*/

func BenchmarkSyncPoolWrapper_GetPutParallel(b *testing.B) {
	//pool := NewSyncPoolWrapper(func() *TestData {
	//	return &TestData{}
	//}, NewStatsRecorder("test"))
	pool := NewSyncPoolWrapper(func() *TestData {
		return &TestData{}
	}, nil)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			pool.Put(pool.Get())
		}
	})
	b.Logf("stats: %+v", pool.Stats())
}

/*
goos: windows
goarch: amd64
pkg: github.com/njtc406/emberengine/engine/pkg/utils/pool
cpu: AMD Ryzen 7 5700X 8-Core Processor
BenchmarkSyncPoolWrapper_GetPutParallel
    pool_test.go:49: stats: {Name:test CurrentSize:1 HitCount:1 MissCount:1 T
otalAlloc:1}
    pool_test.go:49: stats: {Name:test CurrentSize:100 HitCount:100 MissCount
:2 TotalAlloc:2}
    pool_test.go:49: stats: {Name:test CurrentSize:10000 HitCount:10000 MissC
ount:13 TotalAlloc:13}
    pool_test.go:49: stats: {Name:test CurrentSize:1000000 HitCount:1000000 M
issCount:16 TotalAlloc:16}
    pool_test.go:49: stats: {Name:test CurrentSize:43225329 HitCount:43225329
 MissCount:18 TotalAlloc:18}
BenchmarkSyncPoolWrapper_GetPutParallel-16      43225329                27.04 ns
/op
PASS
*/

func BenchmarkPerPPoolWrapper_GetPut(b *testing.B) {
	//pool := NewPerPPoolWrapper(1024, func() *TestData {
	//	return &TestData{}
	//}, NewStatsRecorder("test"))
	pool := NewPerPPoolWrapper(1024, func() *TestData {
		return &TestData{}
	}, nil)
	for i := 0; i < b.N; i++ {
		pool.Put(pool.Get())
	}
	b.Logf("stats: %+v", pool.Stats())
}

/*
goos: windows
goarch: amd64
pkg: github.com/njtc406/emberengine/engine/pkg/utils/pool
cpu: AMD Ryzen 7 2700 Eight-Core Processor
BenchmarkPerPPoolWrapper_GetPut
    pool_test.go:80: stats: {Name:test HitCount:0 MissCount:1 OverflowCount:0
 CurrentSize:2 TotalAlloc:1}
    pool_test.go:80: stats: {Name:test HitCount:99 MissCount:1 OverflowCount:
0 CurrentSize:101 TotalAlloc:1}
    pool_test.go:80: stats: {Name:test HitCount:9999 MissCount:1 OverflowCoun
t:0 CurrentSize:10001 TotalAlloc:1}
    pool_test.go:80: stats: {Name:test HitCount:999999 MissCount:1 OverflowCo
unt:0 CurrentSize:1000001 TotalAlloc:1}
    pool_test.go:80: stats: {Name:test HitCount:61367574 MissCount:2 Overflow
Count:0 CurrentSize:61367578 TotalAlloc:2}
BenchmarkPerPPoolWrapper_GetPut-16      61367576                18.51 ns/op
PASS
*/

func BenchmarkPerPPoolWrapper_GetPutParallel(b *testing.B) {
	//pool := NewPerPPoolWrapper(1024, func() *TestData {
	//	return &TestData{}
	//}, NewStatsRecorder("test"))
	pool := NewPerPPoolWrapper(1024, func() *TestData {
		return &TestData{}
	}, nil)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			pool.Put(pool.Get())
		}
	})
	b.Logf("stats: %+v", pool.Stats())
}

/*
pkg: github.com/njtc406/emberengine/engine/pkg/utils/pool
cpu: AMD Ryzen 7 2700 Eight-Core Processor
BenchmarkPerPPoolWrapper_GetPutParallel
    pool_test.go:112: stats: {Name:test HitCount:0 MissCount:1 OverflowCount:
0 CurrentSize:2 TotalAlloc:1}
    pool_test.go:112: stats: {Name:test HitCount:99 MissCount:1 OverflowCount
:0 CurrentSize:101 TotalAlloc:1}
    pool_test.go:112: stats: {Name:test HitCount:9988 MissCount:12 OverflowCo
unt:0 CurrentSize:10012 TotalAlloc:12}
    pool_test.go:112: stats: {Name:test HitCount:999984 MissCount:16 Overflow
Count:0 CurrentSize:1000016 TotalAlloc:16}
    pool_test.go:112: stats: {Name:test HitCount:20475492 MissCount:16 Overfl
owCount:0 CurrentSize:20475524 TotalAlloc:16}
    pool_test.go:112: stats: {Name:test HitCount:27477208 MissCount:16 Overfl
owCount:0 CurrentSize:27477240 TotalAlloc:16}
BenchmarkPerPPoolWrapper_GetPutParallel-16      27477224                58.61 ns/op
PASS

// 不开统计
BenchmarkPerPPoolWrapper_GetPutParallel-16      599751103                1.950 ns/op
PASS
*/

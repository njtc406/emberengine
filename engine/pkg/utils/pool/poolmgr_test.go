// Package pool
// @Title  title
// @Description  desc
// @Author  yr  2025/7/17
// @Update  yr  2025/7/17
package pool

import "testing"

func BenchmarkSyncPoolWrapper_GetPut(b *testing.B) {
	pool := NewSyncPoolWrapper("test", func() *TestData {
		return &TestData{}
	})
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
    poolmgr_test.go:17: stats: {Name:test CurrentSize:1 HitCount:1 MissCount:0 T
otalAlloc:0}
    poolmgr_test.go:17: stats: {Name:test CurrentSize:100 HitCount:100 MissCount
:0 TotalAlloc:0}
    poolmgr_test.go:17: stats: {Name:test CurrentSize:10000 HitCount:10000 MissC
ount:0 TotalAlloc:0}
    poolmgr_test.go:17: stats: {Name:test CurrentSize:1000000 HitCount:1000000 M
issCount:0 TotalAlloc:0}
    poolmgr_test.go:17: stats: {Name:test CurrentSize:80901238 HitCount:80901238
 MissCount:0 TotalAlloc:0}
BenchmarkSyncPoolWrapper_GetPut-16      80901238                15.07 ns/op
PASS
*/

func BenchmarkSyncPoolWrapper_GetPutParallel(b *testing.B) {
	pool := NewSyncPoolWrapper("test", func() *TestData {
		return &TestData{}
	})
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
    poolmgr_test.go:49: stats: {Name:test CurrentSize:1 HitCount:1 MissCount:1 T
otalAlloc:1}
    poolmgr_test.go:49: stats: {Name:test CurrentSize:100 HitCount:100 MissCount
:2 TotalAlloc:2}
    poolmgr_test.go:49: stats: {Name:test CurrentSize:10000 HitCount:10000 MissC
ount:13 TotalAlloc:13}
    poolmgr_test.go:49: stats: {Name:test CurrentSize:1000000 HitCount:1000000 M
issCount:16 TotalAlloc:16}
    poolmgr_test.go:49: stats: {Name:test CurrentSize:43225329 HitCount:43225329
 MissCount:18 TotalAlloc:18}
BenchmarkSyncPoolWrapper_GetPutParallel-16      43225329                27.04 ns
/op
PASS
*/

package prefixbench

import (
	"strings"
	"testing"
)

var sinkBool bool

func hasPrefixSlice(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

type prefixBucketIndex struct {
	byFirst [256][]string
}

func newPrefixBucketIndex(prefixes []string) *prefixBucketIndex {
	idx := &prefixBucketIndex{}
	for _, p := range prefixes {
		if p == "" {
			continue
		}
		idx.byFirst[p[0]] = append(idx.byFirst[p[0]], p)
	}
	return idx
}

func (idx *prefixBucketIndex) has(s string) bool {
	if s == "" {
		return false
	}
	cands := idx.byFirst[s[0]]
	for _, p := range cands {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

type trieNode struct {
	next map[byte]*trieNode
	end  bool
}

type prefixTrie struct {
	root *trieNode
}

func newPrefixTrie(prefixes []string) *prefixTrie {
	t := &prefixTrie{root: &trieNode{next: make(map[byte]*trieNode)}}
	for _, p := range prefixes {
		if p == "" {
			continue
		}
		n := t.root
		for i := 0; i < len(p); i++ {
			b := p[i]
			n2 := n.next[b]
			if n2 == nil {
				n2 = &trieNode{next: make(map[byte]*trieNode)}
				n.next[b] = n2
			}
			n = n2
		}
		n.end = true
	}
	return t
}

func (t *prefixTrie) has(s string) bool {
	n := t.root
	for i := 0; i < len(s); i++ {
		n = n.next[s[i]]
		if n == nil {
			return false
		}
		if n.end {
			return true
		}
	}
	return false
}

func benchmarkHasPrefix(b *testing.B, has func(string) bool) {
	// 按“前缀数量 <= 5”设计，字符串混合正/负例
	testStrings := []string{
		"ApiPing",
		"APIPing",
		"ApiV2Ping",
		"ApiXHello",
		"RpcCall",
		"RPCStart",
		"RpxTypo",
		"Hello",
		"Ping",
		"",
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkBool = has(testStrings[i%len(testStrings)])
	}
}

func BenchmarkPrefix_Slice_2(b *testing.B) {
	prefixes := []string{"Api", "API"}
	benchmarkHasPrefix(b, func(s string) bool { return hasPrefixSlice(s, prefixes) })
}

func BenchmarkPrefix_Bucket_2(b *testing.B) {
	idx := newPrefixBucketIndex([]string{"Api", "API"})
	benchmarkHasPrefix(b, idx.has)
}

func BenchmarkPrefix_Trie_2(b *testing.B) {
	t := newPrefixTrie([]string{"Api", "API"})
	benchmarkHasPrefix(b, t.has)
}

func BenchmarkPrefix_Slice_5(b *testing.B) {
	prefixes := []string{"Api", "API", "ApiV2", "ApiV3", "ApiX"}
	benchmarkHasPrefix(b, func(s string) bool { return hasPrefixSlice(s, prefixes) })
}

func BenchmarkPrefix_Bucket_5(b *testing.B) {
	idx := newPrefixBucketIndex([]string{"Api", "API", "ApiV2", "ApiV3", "ApiX"})
	benchmarkHasPrefix(b, idx.has)
}

func BenchmarkPrefix_Trie_5(b *testing.B) {
	t := newPrefixTrie([]string{"Api", "API", "ApiV2", "ApiV3", "ApiX"})
	benchmarkHasPrefix(b, t.has)
}

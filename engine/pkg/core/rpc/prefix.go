// Package rpc
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2026/1/29 01:02
// 最后更新:  yr  2026/1/29 01:02
package rpc

import (
	"strings"
)

type prefixBucketIndex struct {
	byFirst [256][]string
}

func newPrefixBucketIndex(prefixes []string) *prefixBucketIndex {
	idx := &prefixBucketIndex{}
	idx.add(prefixes...)
	return idx
}

func (idx *prefixBucketIndex) add(prefixes ...string) {
	for _, p := range prefixes {
		if p == "" {
			continue
		}
		idx.byFirst[p[0]] = append(idx.byFirst[p[0]], p)
	}
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

var (
	apiPrefixIndex = newPrefixBucketIndex([]string{"Api", "API"}) // 只允许node内部调用的方法
	rpcPrefixIndex = newPrefixBucketIndex([]string{"Rpc", "RPC"}) // 允许rpc调用的方法
)

// SetApiPrefix 设置自定义api前缀
func SetApiPrefix(prefix ...string) {
	apiPrefixIndex.add(prefix...)
}

// SetRpcPrefix 设置自定义rpc前缀
func SetRpcPrefix(prefix ...string) {
	rpcPrefixIndex.add(prefix...)
}

func hasApiPrefix(s string) bool {
	return apiPrefixIndex.has(s)
}

func hasRpcPrefix(s string) bool {
	return rpcPrefixIndex.has(s)
}

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
			// CamelCase 边界检查：如果前缀之后还有字符，下一个字符必须是大写字母，
			// 确保正确的驼峰单词边界。
			// 防止 "RpcRo" 误匹配 "RpcRoute"（下一字符 'u' 小写 → 不匹配），
			// 而正确匹配 "RpcRoGetUser"（下一字符 'G' 大写 → 匹配）。
			if len(s) > len(p) {
				next := s[len(p)]
				if next < 'A' || next > 'Z' {
					continue
				}
			}
			return true
		}
	}
	return false
}

var (
	apiPrefixIndex   = newPrefixBucketIndex([]string{"Api", "API"})     // 只允许node内部调用的方法
	rpcPrefixIndex   = newPrefixBucketIndex([]string{"Rpc", "RPC"})     // 允许rpc调用的方法
	apiRoPrefixIndex = newPrefixBucketIndex([]string{"ApiRo", "APIRo"}) // API 只读前缀
	rpcRoPrefixIndex = newPrefixBucketIndex([]string{"RpcRo", "RPCRo"}) // RPC 只读前缀
)

// SetApiPrefix 设置自定义api前缀
func SetApiPrefix(prefix ...string) {
	apiPrefixIndex.add(prefix...)
}

// SetRpcPrefix 设置自定义rpc前缀
func SetRpcPrefix(prefix ...string) {
	rpcPrefixIndex.add(prefix...)
}

// SetApiReadOnlyPrefix 设置自定义的 API 只读前缀
func SetApiReadOnlyPrefix(prefix ...string) {
	apiRoPrefixIndex.add(prefix...)
}

// SetRpcReadOnlyPrefix 设置自定义的 RPC 只读前缀
func SetRpcReadOnlyPrefix(prefix ...string) {
	rpcRoPrefixIndex.add(prefix...)
}

func hasApiPrefix(s string) bool {
	return apiPrefixIndex.has(s)
}

func hasRpcPrefix(s string) bool {
	return rpcPrefixIndex.has(s)
}

// hasApiReadOnlyPrefix 检查方法名是否有 API 只读前缀（ApiRo/APIRo）
func hasApiReadOnlyPrefix(s string) bool {
	return apiRoPrefixIndex.has(s)
}

// hasRpcReadOnlyPrefix 检查方法名是否有 RPC 只读前缀（RpcRo/RPCRo）
func hasRpcReadOnlyPrefix(s string) bool {
	return rpcRoPrefixIndex.has(s)
}

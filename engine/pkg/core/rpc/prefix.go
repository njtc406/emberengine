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

// ── MethodIndex 结构体：持有前缀索引的独立实例 ──

// MethodIndex 管理 API/RPC 方法前缀索引。
// 每个 Node 应持有独立的 MethodIndex 实例。
type MethodIndex struct {
	ApiPrefixIndex   *prefixBucketIndex // 单 node 内部调用的方法(只包含这个前缀的实例不会注册进集群服务发现)
	RpcPrefixIndex   *prefixBucketIndex // 允许 rpc 调用的方法
	ApiRoPrefixIndex *prefixBucketIndex // API 只读前缀(方法中不允许修改数据)
	RpcRoPrefixIndex *prefixBucketIndex // RPC 只读前缀(方法中不允许修改数据)
}

// NewMethodIndex 创建带有默认前缀的 MethodIndex 实例
func NewMethodIndex() *MethodIndex {
	return &MethodIndex{
		ApiPrefixIndex:   newPrefixBucketIndex([]string{"Api", "API"}),
		RpcPrefixIndex:   newPrefixBucketIndex([]string{"Rpc", "RPC"}),
		ApiRoPrefixIndex: newPrefixBucketIndex([]string{"ApiRo", "APIRo"}),
		RpcRoPrefixIndex: newPrefixBucketIndex([]string{"RpcRo", "RPCRo"}),
	}
}

// SetApiPrefix 设置自定义 api 前缀
func (mi *MethodIndex) SetApiPrefix(prefix ...string) {
	mi.ApiPrefixIndex.add(prefix...)
}

// SetRpcPrefix 设置自定义 rpc 前缀
func (mi *MethodIndex) SetRpcPrefix(prefix ...string) {
	mi.RpcPrefixIndex.add(prefix...)
}

// SetApiReadOnlyPrefix 设置自定义的 API 只读前缀
func (mi *MethodIndex) SetApiReadOnlyPrefix(prefix ...string) {
	mi.ApiRoPrefixIndex.add(prefix...)
}

// SetRpcReadOnlyPrefix 设置自定义的 RPC 只读前缀
func (mi *MethodIndex) SetRpcReadOnlyPrefix(prefix ...string) {
	mi.RpcRoPrefixIndex.add(prefix...)
}

func (mi *MethodIndex) HasApiPrefix(s string) bool {
	return mi.ApiPrefixIndex.has(s)
}

func (mi *MethodIndex) HasRpcPrefix(s string) bool {
	return mi.RpcPrefixIndex.has(s)
}

func (mi *MethodIndex) HasApiReadOnlyPrefix(s string) bool {
	return mi.ApiRoPrefixIndex.has(s)
}

func (mi *MethodIndex) HasRpcReadOnlyPrefix(s string) bool {
	return mi.RpcRoPrefixIndex.has(s)
}

// Package metrics 提供最小可用的指标导出能力。
//
// 核心模型：MetricDesc 描述指标元信息，MetricSample 表示一条样本。
// SamplesToText 把任意 []MetricSample 渲染为 Prometheus exposition text。
//
// P1 仅覆盖 PoolMetrics；P2 扩展到 Node/RPC/Mailbox/Event。
package metrics

import (
	"fmt"
	"sort"
	"strings"
)

// MetricType 指标类型
type MetricType int

const (
	Gauge   MetricType = iota // 可增可减的瞬时值
	Counter                   // 单调递增的累计值
)

// MetricDesc 指标描述符
type MetricDesc struct {
	Name string     // 指标名（Prometheus 命名规范：小写+下划线）
	Help string     // 人类可读描述
	Type MetricType // Gauge 或 Counter
}

// MetricSample 单条指标样本
type MetricSample struct {
	Desc   MetricDesc
	Labels map[string]string // label key-value
	Value  float64
}

// SamplesToText 将指标样本列表渲染为 Prometheus exposition text 格式。
//
// 输出保证：
//   - 同名指标连续输出，HELP/TYPE 行只写一次
//   - 调用方应保证同名指标相邻（由各 ToSamples 函数保证）
//   - label 按 key 字母序排列，输出稳定
//
// Content-Type: text/plain; version=0.0.4; charset=utf-8
func SamplesToText(samples []MetricSample) string {
	if len(samples) == 0 {
		return ""
	}

	var b strings.Builder
	lastMetric := ""
	for _, s := range samples {
		if s.Desc.Name != lastMetric {
			fmt.Fprintf(&b, "# HELP %s %s\n", s.Desc.Name, s.Desc.Help)
			fmt.Fprintf(&b, "# TYPE %s %s\n", s.Desc.Name, metricTypeName(s.Desc.Type))
			lastMetric = s.Desc.Name
		}
		fmt.Fprintf(&b, "%s%s %g\n", s.Desc.Name, formatLabels(s.Labels), s.Value)
	}
	return b.String()
}

// --- 内部辅助 ---

func metricTypeName(t MetricType) string {
	switch t {
	case Gauge:
		return "gauge"
	case Counter:
		return "counter"
	default:
		return "untyped"
	}
}

// formatLabels 把 label map 渲染为 {k1="v1",k2="v2"} 格式。
// key 按字母序排列，保证输出稳定。
func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(labels))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf(`%s="%s"`, k, escapeLabelValue(labels[k])))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// escapeLabelValue 转义 Prometheus label value 中的特殊字符
func escapeLabelValue(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

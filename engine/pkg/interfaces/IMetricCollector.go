// Package interfaces
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/11/24 00:16
// 最后更新:  yr  2025/11/24 00:16
package interfaces

import (
	"time"
)

// 模块名: 指标采集器

// TODO: 指标采集器接口
type IMetricCollector interface {
	// 这里应该是从各个地方注册自己的采集器进来，然后在这个接口的实现中去调用他们的收集器
	// 然后统一在实现中分析这些指标
	// 类似下面这种
}

type ICollector interface {
	// 采集器基本信息
	Name() string
	Version() string
	Description() string

	// 生命周期管理
	Initialize(config map[string]interface{}) error
	Start() error
	Stop() error
	IsRunning() bool

	// 数据采集
	Collect() ([]IMetric, error)
	GetMetrics() []IMetric
	GetLastCollectionTime() time.Time
}

type IMetric interface {
	// 指标标识
	Name() string
	Namespace() string
	Description() string
	Type() MetricType

	// 数据值
	Value() interface{}
	Timestamp() time.Time
	Tags() map[string]string

	// 序列化
	//ToPoint() *DataPoint
	//ToJSON() ([]byte, error)
}

type MetricType int

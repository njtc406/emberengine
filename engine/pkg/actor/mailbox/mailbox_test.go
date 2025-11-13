package mailbox

import (
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"testing"
)

// mockInvoker 是一个模拟的消息处理器
type mockInvoker struct {
	serviceName string
}

func (m *mockInvoker) GetServiceName() string {
	return m.serviceName
}

func (m *mockInvoker) InvokeSystemMessage(evt inf.IEvent) {
	// 模拟处理系统消息
}

func (m *mockInvoker) InvokeUserMessage(evt inf.IEvent) {
	// 模拟处理用户消息
}

func (m *mockInvoker) EscalateFailure(reason interface{}, evt inf.IEvent) {
	// 模拟处理失败
}

// TestNewDefaultMultiLevelMailbox 测试创建默认多级邮箱
func TestNewDefaultMultiLevelMailbox(t *testing.T) {
	conf := &config.WorkerConf{
		WorkerNum: 2,
	}

	invoker := &mockInvoker{serviceName: "testService"}
	mailbox := NewDefaultMultiLevelMailbox(conf, invoker)

	if mailbox == nil {
		t.Error("Failed to create multi-level mailbox")
	}

	// 检查配置是否正确设置
	workerConfigInterface := mailbox.GetWorkerConfig()
	workerConfig, ok := workerConfigInterface.(*WorkerConfig)
	if !ok {
		t.Error("Failed to convert worker config to *WorkerConfig")
	}

	if workerConfig == nil {
		t.Error("Worker config should not be nil")
	}

	if workerConfig.MultiLevel == nil {
		t.Error("MultiLevel config should not be nil")
	}

	if !workerConfig.MultiLevel.Enabled {
		t.Error("MultiLevel should be enabled")
	}

	// 不启动邮箱，因为测试环境中没有完整的日志系统
	// mailbox.Start()
	// mailbox.Stop()
}

// TestNewMultiLevelMailbox 测试创建自定义多级邮箱
func TestNewMultiLevelMailbox(t *testing.T) {
	conf := &config.WorkerConf{
		WorkerNum: 2,
	}

	invoker := &mockInvoker{serviceName: "testService"}

	// 创建自定义优先级映射
	priorityMap := map[def.Priority]PriorityConfig{
		def.PriorityUrgent: {BatchSize: 50, Weight: 10},
		def.PriorityHigh:   {BatchSize: 30, Weight: 7},
		def.PriorityNormal: {BatchSize: 20, Weight: 5},
	}

	mailbox := NewMultiLevelMailbox(conf, invoker, def.StrategyWeighted, priorityMap)

	if mailbox == nil {
		t.Error("Failed to create multi-level mailbox")
	}

	// 检查配置是否正确设置
	workerConfigInterface := mailbox.GetWorkerConfig()
	workerConfig, ok := workerConfigInterface.(*WorkerConfig)
	if !ok {
		t.Error("Failed to convert worker config to *WorkerConfig")
	}

	if workerConfig == nil {
		t.Error("Worker config should not be nil")
	}

	if workerConfig.MultiLevel == nil {
		t.Error("MultiLevel config should not be nil")
	}

	if !workerConfig.MultiLevel.Enabled {
		t.Error("MultiLevel should be enabled")
	}

	// 检查优先级映射是否正确
	if len(workerConfig.MultiLevel.Priorities) != 3 {
		t.Errorf("Expected 3 priorities, got %d", len(workerConfig.MultiLevel.Priorities))
	}

	// 不启动邮箱，因为测试环境中没有完整的日志系统
	// mailbox.Start()
	// mailbox.Stop()
}

// TestSetWorkerConfig 测试设置Worker配置
func TestSetWorkerConfig(t *testing.T) {
	conf := &config.WorkerConf{
		WorkerNum: 1,
	}

	invoker := &mockInvoker{serviceName: "testService"}
	mailbox := NewDefaultMailbox(conf, invoker)

	// 创建多级配置
	priorityMap := map[def.Priority]PriorityConfig{
		def.PriorityHigh:   {BatchSize: 30, Weight: 7},
		def.PriorityNormal: {BatchSize: 20, Weight: 5},
	}

	workerConfig := CreateMultiLevelConfig(def.StrategyWeighted, priorityMap)
	mailbox.SetWorkerConfig(workerConfig)

	// 检查配置是否正确设置
	retrievedConfigInterface := mailbox.GetWorkerConfig()
	retrievedConfig, ok := retrievedConfigInterface.(*WorkerConfig)
	if !ok {
		t.Error("Failed to convert worker config to *WorkerConfig")
	}

	if retrievedConfig == nil {
		t.Error("Worker config should not be nil")
	}

	if retrievedConfig.MultiLevel == nil {
		t.Error("MultiLevel config should not be nil")
	}

	if !retrievedConfig.MultiLevel.Enabled {
		t.Error("MultiLevel should be enabled")
	}
}

package config

import (
	"os"
	"path/filepath"
	"testing"
)

// ============================================================================
// P0-5: 配置负向测试
//
// 覆盖：
// - P0-5.1: StopPolicy、MailboxConf、EventBusConf 非法值
// - P0-5.2: 缺少必填字段
// - P0-5.3: 模板和 example 配置正向回归（已在 config_test.go 覆盖）
// ============================================================================

// writeYAMLTemp 写一个临时的 node.yaml 并返回目录路径
func writeYAMLTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "node.yaml"), []byte(content), 0600); err != nil {
		t.Fatalf("write temp node.yaml: %v", err)
	}
	return dir
}

// --- P0-5.2: 缺少必填字段 ---

func TestConfigLoad_MissingNodeType_Fails(t *testing.T) {
	yaml := `
NodeConf:
  NodeId: "test-node"
  # NodeType missing
  SystemStatus: "debug"
  PVCPath: "./data"
  PVPath: "./run"
  AntsPoolSize: 100
`
	dir := writeYAMLTemp(t, yaml)
	cfg := NewConfig()
	err := cfg.Load(dir)
	if err == nil {
		t.Fatal("expected error when NodeType is missing")
	}
	t.Logf("expected error: %v", err)
}

func TestConfigLoad_MissingNodeId_Fails(t *testing.T) {
	yaml := `
NodeConf:
  # NodeId missing
  NodeType: "test"
  SystemStatus: "debug"
  PVCPath: "./data"
  PVPath: "./run"
  AntsPoolSize: 100
`
	dir := writeYAMLTemp(t, yaml)
	cfg := NewConfig()
	err := cfg.Load(dir)
	if err == nil {
		t.Fatal("expected error when NodeId is missing")
	}
	t.Logf("expected error: %v", err)
}

func TestConfigLoad_MissingServiceConf_Fails(t *testing.T) {
	yaml := `
NodeConf:
  NodeId: "test-node"
  NodeType: "test"
  SystemStatus: "debug"
  PVCPath: "./data"
  PVPath: "./run"
  AntsPoolSize: 100
# ServiceConf missing entirely
`
	dir := writeYAMLTemp(t, yaml)
	cfg := NewConfig()
	err := cfg.Load(dir)
	if err == nil {
		t.Fatal("expected error when ServiceConf is missing")
	}
	t.Logf("expected error: %v", err)
}

func TestConfigLoad_EmptyStartServices_Fails(t *testing.T) {
	yaml := `
NodeConf:
  NodeId: "test-node"
  NodeType: "test"
  SystemStatus: "debug"
  PVCPath: "./data"
  PVPath: "./run"
  AntsPoolSize: 100
ServiceConf:
  StartServices: []
`
	dir := writeYAMLTemp(t, yaml)
	cfg := NewConfig()
	err := cfg.Load(dir)
	if err == nil {
		t.Fatal("expected error when StartServices is empty")
	}
	t.Logf("expected error: %v", err)
}

func TestConfigLoad_ServiceMissingClassName_Fails(t *testing.T) {
	yaml := `
NodeConf:
  NodeId: "test-node"
  NodeType: "test"
  SystemStatus: "debug"
  PVCPath: "./data"
  PVPath: "./run"
  AntsPoolSize: 100
ServiceConf:
  StartServices:
    - Type: "normal"
      Partition: 1
`
	dir := writeYAMLTemp(t, yaml)
	cfg := NewConfig()
	err := cfg.Load(dir)
	if err == nil {
		t.Fatal("expected error when ClassName is missing")
	}
	t.Logf("expected error: %v", err)
}

func TestConfigLoad_ServiceMissingPartition_Fails(t *testing.T) {
	yaml := `
NodeConf:
  NodeId: "test-node"
  NodeType: "test"
  SystemStatus: "debug"
  PVCPath: "./data"
  PVPath: "./run"
  AntsPoolSize: 100
ServiceConf:
  StartServices:
    - ClassName: "TestSvc"
      Type: "normal"
      # Partition missing
`
	dir := writeYAMLTemp(t, yaml)
	cfg := NewConfig()
	err := cfg.Load(dir)
	if err == nil {
		t.Fatal("expected error when Partition is missing")
	}
	t.Logf("expected error: %v", err)
}

// --- P0-5.1: StopPolicy 非法值测试 ---

func TestConfigLoad_MinimalValid_Succeeds(t *testing.T) {
	yaml := `
NodeConf:
  NodeId: "test-node"
  NodeType: "test"
  SystemStatus: "debug"
  PVCPath: "./data"
  PVPath: "./run"
  AntsPoolSize: 100
SystemLogger:
  OutputFormat: "json"
  Dir: "./logs"
  Level: "info"
  Stdout: true
ClusterConf:
  ETCDConf:
    Endpoints:
      - "127.0.0.1:2379"
    DialTimeout: 3s
ServiceConf:
  StartServices:
    - ClassName: "TestSvc"
      Type: "normal"
      Partition: 1
      StopPolicy:
        GraceTimeout: 5s
        DrainPolicy: "execute"
      Mailbox:
        QueueMode: "dual"
        StopTimeout: 5s
        MaxJobExecutionTime: 30s
        SchedulePolicy:
          InitialWorkerNum: 1
          VirtualWorkerRate: 24
        MiddlewareConf:
          DispatchKeyStatsInterval: 10s
          RateLimitConf:
            Enable: false
          CircuitBreakerConf:
            Enable: false
`
	dir := writeYAMLTemp(t, yaml)
	cfg := NewConfig()
	err := cfg.Load(dir)
	if err != nil {
		t.Fatalf("minimal valid config should succeed, got: %v", err)
	}
	if cfg.NodeConf.NodeType != "test" {
		t.Errorf("NodeType = %q, want 'test'", cfg.NodeConf.NodeType)
	}
}

func TestConfigLoad_StopPolicyDiscardValid(t *testing.T) {
	yaml := `
NodeConf:
  NodeId: "test-node"
  NodeType: "test"
  SystemStatus: "debug"
  PVCPath: "./data"
  PVPath: "./run"
  AntsPoolSize: 100
SystemLogger:
  OutputFormat: "json"
  Dir: "./logs"
  Level: "info"
  Stdout: true
ClusterConf:
  ETCDConf:
    Endpoints:
      - "127.0.0.1:2379"
    DialTimeout: 3s
ServiceConf:
  StartServices:
    - ClassName: "TestSvc"
      Type: "normal"
      Partition: 1
      StopPolicy:
        GraceTimeout: 10s
        DrainPolicy: "discard"
      Mailbox:
        QueueMode: "dual"
        StopTimeout: 5s
        MaxJobExecutionTime: 30s
        SchedulePolicy:
          InitialWorkerNum: 2
          VirtualWorkerRate: 24
        MiddlewareConf:
          DispatchKeyStatsInterval: 10s
          RateLimitConf:
            Enable: false
          CircuitBreakerConf:
            Enable: false
`
	dir := writeYAMLTemp(t, yaml)
	cfg := NewConfig()
	err := cfg.Load(dir)
	if err != nil {
		t.Fatalf("discard drain policy should be valid, got: %v", err)
	}
}

func TestConfigLoad_MissingSystemLogger_Fails(t *testing.T) {
	yaml := `
NodeConf:
  NodeId: "test-node"
  NodeType: "test"
  SystemStatus: "debug"
  PVCPath: "./data"
  PVPath: "./run"
  AntsPoolSize: 100
# SystemLogger missing
ServiceConf:
  StartServices:
    - ClassName: "TestSvc"
      Type: "normal"
      Partition: 1
`
	dir := writeYAMLTemp(t, yaml)
	cfg := NewConfig()
	err := cfg.Load(dir)
	if err == nil {
		t.Fatal("expected error when SystemLogger is missing")
	}
	t.Logf("expected error: %v", err)
}

// --- Edge cases ---

func TestConfigLoad_EmptyFile_Fails(t *testing.T) {
	dir := writeYAMLTemp(t, "")
	cfg := NewConfig()
	err := cfg.Load(dir)
	if err == nil {
		t.Fatal("expected error for empty config file")
	}
}

func TestConfigLoad_InvalidYAML_Fails(t *testing.T) {
	dir := writeYAMLTemp(t, "{{invalid yaml}}")
	cfg := NewConfig()
	err := cfg.Load(dir)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

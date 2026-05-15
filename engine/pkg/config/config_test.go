package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/viper"
)

func TestConfigSetStatusAndIsDebug(t *testing.T) {
	cfg := NewConfig()

	if cfg.IsDebug() {
		t.Fatalf("expected IsDebug=false when NodeConf is nil")
	}

	cfg.SetStatus("DEBUG")
	if cfg.GetStatus() != Debug {
		t.Fatalf("expected status=%q after SetStatus(DEBUG), got=%q", Debug, cfg.GetStatus())
	}
	if !cfg.IsDebug() {
		t.Fatalf("expected IsDebug=true after setting debug status")
	}

	cfg.SetStatus("release")
	if cfg.GetStatus() != Release {
		t.Fatalf("expected status=%q after SetStatus(release), got=%q", Release, cfg.GetStatus())
	}
	if cfg.IsDebug() {
		t.Fatalf("expected IsDebug=false in release mode")
	}
}

func TestConfigSetStatusIgnoresInvalidValue(t *testing.T) {
	cfg := NewConfig()
	cfg.SetStatus(Debug)

	cfg.SetStatus("invalid")
	if cfg.GetStatus() != Debug {
		t.Fatalf("expected status unchanged on invalid input, got=%q", cfg.GetStatus())
	}
}

func TestConfigRpcTimeoutFallbackAndOverride(t *testing.T) {
	cfg := NewConfig()

	if got := cfg.GetDefaultRpcTimeout(); got != def.DefaultRpcTimeout {
		t.Fatalf("expected default rpc timeout=%s, got=%s", def.DefaultRpcTimeout, got)
	}
	if got := cfg.GetCheckTimeoutInterval(); got != def.DefaultCheckRpcCallTimeoutInterval {
		t.Fatalf("expected default check interval=%s, got=%s", def.DefaultCheckRpcCallTimeoutInterval, got)
	}

	cfg.NodeConf = &NodeConf{
		RpcMonitorConf: &RpcMonitorConf{
			DefaultRpcTimeout:    3 * time.Second,
			CheckTimeoutInterval: 250 * time.Millisecond,
		},
	}
	if got := cfg.GetDefaultRpcTimeout(); got != 3*time.Second {
		t.Fatalf("expected overridden rpc timeout=3s, got=%s", got)
	}
	if got := cfg.GetCheckTimeoutInterval(); got != 250*time.Millisecond {
		t.Fatalf("expected overridden check interval=250ms, got=%s", got)
	}
}

func TestConfigServiceConfRegistry(t *testing.T) {
	cfg := NewConfig()

	type demoCfg struct{ Name string }
	creatorA := func() interface{} { return &demoCfg{Name: "A"} }
	creatorB := func() interface{} { return &demoCfg{Name: "B"} }

	svcA := &ServiceConfig{ServiceName: "svcA", CfgCreator: creatorA}
	svcB := &ServiceConfig{ServiceName: "svcB", CfgCreator: creatorB}

	cfg.RegisterServiceConf(svcA, svcB)

	if got := cfg.GetServiceConf("svcA"); got == nil {
		t.Fatalf("expected service config creator for svcA")
	} else {
		fn, ok := got.(func() interface{})
		if !ok {
			t.Fatalf("expected svcA creator function type, got %T", got)
		}
		inst, _ := fn().(*demoCfg)
		if inst == nil || inst.Name != "A" {
			t.Fatalf("expected creator result Name=A, got=%+v", inst)
		}
	}

	if got := cfg.GetServiceConf("svcB"); got == nil {
		t.Fatalf("expected service config creator for svcB")
	}

	if got := cfg.GetServiceConf("missing"); got != nil {
		t.Fatalf("expected nil for missing service config, got %T", got)
	}
}

func TestConfigDiscoveryConfRegistry(t *testing.T) {
	cfg := NewConfig()

	custom := map[string]interface{}{"path": "/ember/discovery", "ttl": 3}
	cfg.RegisterDiscoveryConf("etcd", custom)

	got := cfg.GetDiscoveryConf("etcd")
	if got == nil {
		t.Fatalf("expected discovery config for etcd")
	}
	m, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map discovery config, got %T", got)
	}
	if m["path"] != "/ember/discovery" || m["ttl"] != 3 {
		t.Fatalf("unexpected discovery config content: %+v", m)
	}

	if miss := cfg.GetDiscoveryConf("missing"); miss != nil {
		t.Fatalf("expected nil for missing discovery config, got %T", miss)
	}
}

func TestConfigRegisterServiceConfOverwritesByName(t *testing.T) {
	cfg := NewConfig()

	first := &ServiceConfig{
		ServiceName: "dup",
		CfgCreator: func() interface{} {
			return "first"
		},
	}
	second := &ServiceConfig{
		ServiceName: "dup",
		CfgCreator: func() interface{} {
			return "second"
		},
		DefaultSetFun: func(*viper.Viper) {},
	}

	cfg.RegisterServiceConf(first)
	cfg.RegisterServiceConf(second)

	got := cfg.GetServiceConf("dup")
	if got == nil {
		t.Fatalf("expected config for duplicated service name")
	}
	fn, ok := got.(func() interface{})
	if !ok {
		t.Fatalf("expected creator function type, got %T", got)
	}
	if v := fn(); v != "second" {
		t.Fatalf("expected latest registration to win, got=%v", v)
	}
}

func TestConfigLoadRepositoryTemplateAndActorExamples(t *testing.T) {
	repoRoot := findRepoRoot(t)
	t.Setenv("REMOTE_HOST", "127.0.0.1")
	t.Setenv("EMBER_CONF_PATH", "")

	cases := []struct {
		name      string
		configDir string
	}{
		{name: "template", configDir: "template/config"},
		{name: "node1", configDir: "example/configs/node1"},
		{name: "node2", configDir: "example/configs/node2"},
		{name: "node3", configDir: "example/configs/node3"},
		{name: "node_concurrency", configDir: "example/configs/node_concurrency"},
		{name: "node_concurrency1", configDir: "example/configs/node_concurrency1"},
		{name: "node_local", configDir: "example/configs/node_local"},
		{name: "node_master", configDir: "example/configs/node_master"},
		{name: "node_slave", configDir: "example/configs/node_slave"},
		{name: "node_slave1", configDir: "example/configs/node_slave1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			confDir := copyNodeConfigToTempDir(t, repoRoot, tc.configDir)
			cfg := NewConfig()
			if err := cfg.Load(confDir); err != nil {
				t.Fatalf("load %s: %v", tc.configDir, err)
			}

			if cfg.NodeConf == nil || cfg.NodeConf.NodeType == "" {
				t.Fatalf("%s must set NodeConf.NodeType", tc.configDir)
			}
			if cfg.SystemLogger == nil || cfg.SystemLogger.OutputFormat == "" {
				t.Fatalf("%s must set SystemLogger.OutputFormat", tc.configDir)
			}
			if cfg.ServiceConf == nil || len(cfg.ServiceConf.StartServices) == 0 {
				t.Fatalf("%s must define at least one start service", tc.configDir)
			}

			for _, service := range cfg.ServiceConf.StartServices {
				if service.StopPolicy == nil {
					t.Fatalf("service %s in %s must set StopPolicy", service.ClassName, tc.configDir)
				}
				if service.Mailbox == nil {
					continue
				}
				assertActorMailboxConfig(t, tc.configDir, service.ClassName, service.Mailbox)
			}
		})
	}
}

func assertActorMailboxConfig(t *testing.T, configDir, className string, mailbox *MailboxConf) {
	t.Helper()
	if mailbox.QueueMode == "" {
		t.Fatalf("service %s in %s must set Mailbox.QueueMode", className, configDir)
	}
	if mailbox.StopTimeout <= 0 {
		t.Fatalf("service %s in %s must set positive Mailbox.StopTimeout", className, configDir)
	}
	if mailbox.MaxJobExecutionTime <= 0 {
		t.Fatalf("service %s in %s must set positive Mailbox.MaxJobExecutionTime", className, configDir)
	}
	if mailbox.MiddlewareConf == nil {
		t.Fatalf("service %s in %s must set Mailbox.MiddlewareConf", className, configDir)
	}
	if mailbox.MiddlewareConf.DispatchKeyStatsInterval <= 0 {
		t.Fatalf("service %s in %s must set positive DispatchKeyStatsInterval", className, configDir)
	}
	if mailbox.MiddlewareConf.RateLimitConf == nil {
		t.Fatalf("service %s in %s must set RateLimitConf", className, configDir)
	}
	if mailbox.MiddlewareConf.CircuitBreakerConf == nil {
		t.Fatalf("service %s in %s must set CircuitBreakerConf", className, configDir)
	}
}

func copyNodeConfigToTempDir(t *testing.T, repoRoot, configDir string) string {
	t.Helper()

	source := filepath.Join(repoRoot, filepath.FromSlash(configDir), "node.yaml")
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read %s: %v", source, err)
	}

	tempDir := t.TempDir()
	dataDir := filepath.ToSlash(filepath.Join(tempDir, "data"))
	cacheDir := filepath.ToSlash(filepath.Join(tempDir, "cache"))
	logDir := filepath.ToSlash(filepath.Join(tempDir, "logs"))
	content := string(raw)
	content = strings.ReplaceAll(content, "PVCPath: ./example/data", "PVCPath: "+dataDir)
	content = strings.ReplaceAll(content, "PVPath: ./example/cache", "PVPath: "+cacheDir)
	content = strings.ReplaceAll(content, "Dir: ./example/data/logs", "Dir: "+logDir)

	if err := os.WriteFile(filepath.Join(tempDir, "node.yaml"), []byte(content), 0600); err != nil {
		t.Fatalf("write temp node.yaml: %v", err)
	}
	return tempDir
}

func findRepoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found while walking parent directories")
		}
		dir = parent
	}
}

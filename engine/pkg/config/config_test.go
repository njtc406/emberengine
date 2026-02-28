package config

import (
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

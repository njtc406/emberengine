package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/njtc406/emberengine/v2/engine/pkg/config"
	"github.com/njtc406/emberengine/v2/engine/pkg/log"
)

func TestStartWithNilConfLoadsValidFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EMBER_CONF_PATH", "")
	cfg := &config.Config{
		NodeConf:     &config.NodeConf{NodeId: "file", NodeType: "test", SystemStatus: config.Debug, PVCPath: dir, PVPath: filepath.Join(dir, "run"), AntsPoolSize: 16},
		SystemLogger: &log.LoggerConf{Level: "error", OutputFormat: "text"},
		ClusterConf:  &config.ClusterConf{ETCDConf: &config.ETCDConf{}},
		ServiceConf:  &config.ServiceConf{StartServices: []*config.ServiceInitConf{}, ServicesConfMap: map[string]*config.ServiceConfig{}},
	}
	// JSON is a YAML subset and avoids platform-specific path escaping.
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node.yaml"), data, 0600); err != nil {
		t.Fatal(err)
	}
	n, err := New().Start(WithConf(nil), WithConfPath(dir))
	if err != nil {
		t.Fatalf("start from file: %v", err)
	}
	defer n.Logger.Close()
	defer n.Stop()
	if n.Config.NodeConf.NodeId != "file" || !n.IsReady() {
		t.Fatal("file configuration was not used")
	}
}

func TestStartWithConfPreservesServiceConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EMBER_CONF_PATH", filepath.Join(dir, "missing"))
	service := &config.ServiceConfig{Cfg: map[string]string{"source": "external"}}
	cfg := &config.Config{
		NodeConf:     &config.NodeConf{NodeId: "services", NodeType: "test", SystemStatus: config.Debug, PVCPath: dir, PVPath: dir, AntsPoolSize: 16},
		SystemLogger: &log.LoggerConf{Level: "error", OutputFormat: "text"},
		ClusterConf:  &config.ClusterConf{ETCDConf: &config.ETCDConf{}},
		ServiceConf:  &config.ServiceConf{OpenRemote: true, StartServices: []*config.ServiceInitConf{}, ServicesConfMap: map[string]*config.ServiceConfig{"demo": service}},
	}
	n, err := New().Start(WithConf(cfg))
	if err != nil {
		t.Fatalf("start without remote configuration loading: %v", err)
	}
	defer n.Logger.Close()
	defer n.Stop()
	if n.Config.ServiceConf.ServicesConfMap["demo"] != service {
		t.Fatal("external service configuration replaced")
	}
}

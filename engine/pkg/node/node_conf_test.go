package node

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/njtc406/emberengine/v2/engine/pkg/config"
	"github.com/njtc406/emberengine/v2/engine/pkg/log"
)

func TestStartWithConf(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(map[bool]string{false: "conf-last", true: "path-last"}[reverse], func(t *testing.T) {
			dir := t.TempDir()
			missing := filepath.Join(dir, "missing")
			t.Setenv("EMBER_CONF_PATH", missing)
			cfg := &config.Config{
				NodeConf:     &config.NodeConf{NodeId: "injected", NodeType: "test", SystemStatus: config.Debug, PVCPath: dir, PVPath: filepath.Join(dir, "run"), AntsPoolSize: 16},
				SystemLogger: &log.LoggerConf{Level: "error", OutputFormat: "text", Dir: filepath.Join(dir, "logs")},
				ClusterConf:  &config.ClusterConf{ETCDConf: &config.ETCDConf{}},
				ServiceConf:  &config.ServiceConf{StartServices: []*config.ServiceInitConf{}, ServicesConfMap: map[string]*config.ServiceConfig{}},
			}
			opts := []StartOption{WithConfPath(missing), WithConf(cfg)}
			if reverse {
				opts[0], opts[1] = opts[1], opts[0]
			}
			n, err := New().Start(opts...)
			if err != nil {
				t.Fatalf("start with injected config: %v", err)
			}
			defer n.Logger.Close()
			defer n.Stop()
			if n.Config != cfg {
				t.Fatal("injected config was replaced")
			}
			if !n.IsReady() {
				t.Fatal("node is not ready")
			}
			for _, path := range []string{cfg.NodeConf.PVPath, cfg.SystemLogger.Dir} {
				if info, err := os.Stat(path); err != nil || !info.IsDir() {
					t.Fatalf("directory not initialized: %s (%v)", path, err)
				}
			}
		})
	}
}

func TestStartWithoutConfLoadsFile(t *testing.T) {
	t.Setenv("EMBER_CONF_PATH", t.TempDir())
	for _, opts := range [][]StartOption{nil, {WithConf(nil)}} {
		_, err := New().Start(opts...)
		if err == nil || !strings.Contains(err.Error(), "read node.yaml") {
			t.Fatalf("expected file loading error, got %v", err)
		}
	}
}

func TestStartWithInvalidConfDoesNotFallBack(t *testing.T) {
	t.Setenv("EMBER_CONF_PATH", t.TempDir())
	_, err := New().Start(WithConf(&config.Config{}))
	if err == nil || !strings.Contains(err.Error(), "config validation") {
		t.Fatalf("expected validation error, got %v", err)
	}
	if strings.Contains(err.Error(), "node.yaml") {
		t.Fatalf("unexpected file fallback: %v", err)
	}
}

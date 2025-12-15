package log

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap/zapcore"
)

func TestProductionOutputsJSON(t *testing.T) {
	var buf bytes.Buffer

	oldFactory := stdoutWriteSyncerFactory
	stdoutWriteSyncerFactory = func(_ bool) zapcore.WriteSyncer {
		return zapcore.AddSync(&buf)
	}
	t.Cleanup(func() { stdoutWriteSyncerFactory = oldFactory })

	logger, err := NewDefaultLogger(&LoggerConf{
		OutputFormat: "json",
		Stdout:       true,
		PrefixName:   "",
		Level:        "info",
		Caller:       false,
		Color:        true, // should be ignored when OutputFormat=json
	})
	if err != nil {
		t.Fatalf("NewDefaultLogger error: %v", err)
	}
	defer Release(logger)

	logger.WithField("k", "v").Info("hello")
	_ = logger.Close()

	out := strings.TrimSpace(buf.String())
	if out == "" {
		t.Fatalf("expected output, got empty")
	}
	if !strings.HasPrefix(out, "{") {
		t.Fatalf("expected json object, got: %q", out)
	}
	if !strings.Contains(out, "\"msg\"") || !strings.Contains(out, "hello") {
		t.Fatalf("expected msg field in output, got: %q", out)
	}
	if !strings.Contains(out, "\"k\"") || !strings.Contains(out, "v") {
		t.Fatalf("expected custom field in output, got: %q", out)
	}
}

func TestNonProductionOutputsLegacyText(t *testing.T) {
	var buf bytes.Buffer

	oldFactory := stdoutWriteSyncerFactory
	stdoutWriteSyncerFactory = func(_ bool) zapcore.WriteSyncer {
		return zapcore.AddSync(&buf)
	}
	t.Cleanup(func() { stdoutWriteSyncerFactory = oldFactory })

	logger, err := NewDefaultLogger(&LoggerConf{
		OutputFormat: "text",
		Stdout:       true,
		PrefixName:   "",
		Level:        "info",
		Caller:       false,
		Color:        false,
	})
	if err != nil {
		t.Fatalf("NewDefaultLogger error: %v", err)
	}
	defer Release(logger)

	logger.Info("hello")
	_ = logger.Close()

	out := buf.String()
	if !strings.Contains(out, ">> hello") {
		t.Fatalf("expected legacy marker in output, got: %q", strings.TrimSpace(out))
	}
}

func TestProductionOutputsJSONToFile(t *testing.T) {
	dir := t.TempDir()

	logger, err := NewDefaultLogger(&LoggerConf{
		OutputFormat: "json",
		Stdout:       false,
		Dir:          dir,
		PrefixName:   "app",
		Level:        "info",
		Caller:       false,
		Color:        true, // should be ignored when OutputFormat=json
		Rotation: &RotationConf{
			MaxAge: 24 * time.Hour,
			Every:  24 * time.Hour,
		},
		Routing: &RoutingConf{
			AsyncMode: &AsyncMode{Enable: false},
			Routes: []LevelRoute{{
				Name:   "access.log",
				Levels: AllLevelStrs,
			}},
		},
	})
	if err != nil {
		t.Fatalf("NewDefaultLogger error: %v", err)
	}
	defer Release(logger)

	logger.WithField("k", "v").Info("hello")
	_ = logger.Close()

	pattern := filepath.Join(dir, "app_access.log.*")
	matches, _ := filepath.Glob(pattern)
	if len(matches) == 0 {
		t.Fatalf("expected log file matching %s", pattern)
	}

	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	line := strings.TrimSpace(string(data))
	if line == "" {
		t.Fatalf("expected file content, got empty")
	}
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	if !strings.HasPrefix(line, "{") {
		t.Fatalf("expected json object line, got: %q", line)
	}
	if !strings.Contains(line, "\"msg\"") || !strings.Contains(line, "hello") {
		t.Fatalf("expected msg field in output, got: %q", line)
	}
	if !strings.Contains(line, "\"k\"") || !strings.Contains(line, "v") {
		t.Fatalf("expected custom field in output, got: %q", line)
	}
}

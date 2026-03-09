package core

import (
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
)

func TestFixConf_AppliesDefaults(t *testing.T) {
	conf := &config.ServiceInitConf{}

	got := fixConf(conf)
	if got != conf {
		t.Fatalf("expected fixConf to mutate and return original pointer")
	}
	if conf.Type != "Normal" {
		t.Fatalf("expected default Type=Normal, got=%q", conf.Type)
	}
	if conf.RpcType != def.RpcTypeNats {
		t.Fatalf("expected default RpcType=%q, got=%q", def.RpcTypeNats, conf.RpcType)
	}
	if conf.LogConf == nil || conf.LogConf.Enable {
		t.Fatalf("expected default LogConf with Enable=false")
	}
	if conf.TimerConf == nil {
		t.Fatalf("expected default TimerConf")
	}
	if conf.TimerConf.TimerSize != def.DefaultTimerSize {
		t.Fatalf("expected default TimerSize=%d, got=%d", def.DefaultTimerSize, conf.TimerConf.TimerSize)
	}
	if conf.TimerConf.TimerBucketSize != def.DefaultTimerBucketSize {
		t.Fatalf("expected default TimerBucketSize=%d, got=%d", def.DefaultTimerBucketSize, conf.TimerConf.TimerBucketSize)
	}
	if conf.EventChanSize != def.DefaultEventChanSize {
		t.Fatalf("expected default EventChanSize=%d, got=%d", def.DefaultEventChanSize, conf.EventChanSize)
	}
}

func TestFixConf_NormalizesNegativeStopTimeouts(t *testing.T) {
	conf := &config.ServiceInitConf{
		StopGraceTimeout: -1 * time.Second,
		StopPolicy: &config.StopPolicyConf{
			GraceTimeout: -2 * time.Second,
		},
	}

	fixConf(conf)
	if conf.StopGraceTimeout != 0 {
		t.Fatalf("expected StopGraceTimeout normalized to 0, got=%s", conf.StopGraceTimeout)
	}
	if conf.StopPolicy == nil || conf.StopPolicy.GraceTimeout != 0 {
		t.Fatalf("expected StopPolicy.GraceTimeout normalized to 0, got=%v", conf.StopPolicy)
	}
}

func TestFixConf_PreservesProvidedValues(t *testing.T) {
	conf := &config.ServiceInitConf{
		Type:          "Worker",
		RpcType:       def.RpcTypeGrpc,
		EventChanSize: 256,
		TimerConf: &config.TimerConf{
			TimerSize:       64,
			TimerBucketSize: 32,
		},
		LogConf: &config.ServiceLogConf{Enable: true},
	}

	fixConf(conf)
	if conf.Type != "Worker" {
		t.Fatalf("expected Type to be preserved, got=%q", conf.Type)
	}
	if conf.RpcType != def.RpcTypeGrpc {
		t.Fatalf("expected RpcType to be preserved, got=%q", conf.RpcType)
	}
	if conf.EventChanSize != 256 {
		t.Fatalf("expected EventChanSize to be preserved, got=%d", conf.EventChanSize)
	}
	if conf.TimerConf == nil || conf.TimerConf.TimerSize != 64 || conf.TimerConf.TimerBucketSize != 32 {
		t.Fatalf("expected TimerConf to be preserved, got=%+v", conf.TimerConf)
	}
	if conf.LogConf == nil || !conf.LogConf.Enable {
		t.Fatalf("expected LogConf to be preserved")
	}
}

func TestFixConf_NormalizesNonPositiveTimerAndEventValues(t *testing.T) {
	conf := &config.ServiceInitConf{
		TimerConf: &config.TimerConf{
			TimerSize:       0,
			TimerBucketSize: -1,
		},
		EventChanSize: 0,
	}

	fixConf(conf)
	if conf.TimerConf == nil {
		t.Fatalf("expected TimerConf not nil")
	}
	if conf.TimerConf.TimerSize != def.DefaultTimerSize {
		t.Fatalf("expected TimerSize normalized to default=%d, got=%d", def.DefaultTimerSize, conf.TimerConf.TimerSize)
	}
	if conf.TimerConf.TimerBucketSize != def.DefaultTimerBucketSize {
		t.Fatalf("expected TimerBucketSize normalized to default=%d, got=%d", def.DefaultTimerBucketSize, conf.TimerConf.TimerBucketSize)
	}
	if conf.EventChanSize != def.DefaultEventChanSize {
		t.Fatalf("expected EventChanSize normalized to default=%d, got=%d", def.DefaultEventChanSize, conf.EventChanSize)
	}
}

func TestFixConf_PreservesPositiveStopPolicyTimeout(t *testing.T) {
	conf := &config.ServiceInitConf{
		StopGraceTimeout: 3 * time.Second,
		StopPolicy: &config.StopPolicyConf{
			GraceTimeout: 5 * time.Second,
		},
	}

	fixConf(conf)
	if conf.StopGraceTimeout != 3*time.Second {
		t.Fatalf("expected StopGraceTimeout preserved, got=%s", conf.StopGraceTimeout)
	}
	if conf.StopPolicy == nil || conf.StopPolicy.GraceTimeout != 5*time.Second {
		t.Fatalf("expected StopPolicy.GraceTimeout preserved, got=%v", conf.StopPolicy)
	}
}

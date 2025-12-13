package log

import (
	"io"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func newBenchFormatCore(withCaller bool, withBaseFields int) *formatCore {
	core := newFormatCore(
		zapcore.AddSync(io.Discard),
		zapcore.DebugLevel,
		levelEnablerAll(zapcore.DebugLevel),
		withCaller,
		false,
		colorNone,
		false,
	)
	fc := core.(*formatCore)
	if withBaseFields > 0 {
		base := make([]zapcore.Field, 0, withBaseFields)
		for i := 0; i < withBaseFields; i++ {
			base = append(base, zap.Int("bf", i))
		}
		fc.baseFields = base
	}
	return fc
}

func BenchmarkFormatCoreWrite_NoFields_NoCaller(b *testing.B) {
	b.ReportAllocs()

	c := newBenchFormatCore(false, 0)
	ent := zapcore.Entry{
		Level:   zapcore.InfoLevel,
		Message: "hello",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = c.Write(ent, nil)
	}
}

func BenchmarkFormatCoreWrite_10Fields_NoCaller(b *testing.B) {
	b.ReportAllocs()

	c := newBenchFormatCore(false, 0)
	ent := zapcore.Entry{
		Level:   zapcore.InfoLevel,
		Message: "hello",
	}
	fields := make([]zapcore.Field, 0, 10)
	for i := 0; i < 10; i++ {
		fields = append(fields, zap.Int("k", i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = c.Write(ent, fields)
	}
}

func BenchmarkFormatCoreWrite_10Fields_WithCaller(b *testing.B) {
	b.ReportAllocs()

	c := newBenchFormatCore(true, 0)
	ent := zapcore.Entry{
		Level:   zapcore.InfoLevel,
		Message: "hello",
		Caller: zapcore.EntryCaller{
			Defined: true,
			File:    "F:/go/src/emberengine/engine/pkg/log/zap_core_bench_test.go",
			Line:    123,
		},
	}
	fields := make([]zapcore.Field, 0, 10)
	for i := 0; i < 10; i++ {
		fields = append(fields, zap.Int("k", i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = c.Write(ent, fields)
	}
}

func BenchmarkFormatCoreWrite_WithBaseFields(b *testing.B) {
	b.ReportAllocs()

	c := newBenchFormatCore(false, 8)
	ent := zapcore.Entry{
		Level:   zapcore.InfoLevel,
		Message: "hello",
	}
	fields := []zapcore.Field{
		zap.String("tag", "slow"),
		zap.String("k", "v"),
		zap.Int("n", 42),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = c.Write(ent, fields)
	}
}

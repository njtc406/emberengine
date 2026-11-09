package rpc

import (
	"fmt"
	inf "github.com/njtc406/emberengine/v2/engine/pkg/interfaces"
	"testing"
)

type partitionNumber int64
type partitionText string

func (partitionNumber) String() string { return "wrong" }
func (partitionText) String() string   { return "wrong" }

func TestWithPartitionCanonicalString(t *testing.T) {
	tests := []struct {
		name   string
		option inf.SelectParamBuilder
		want   string
	}{
		{"int", WithPartition(1), "1"},
		{"int64", WithPartition(int64(1)), "1"},
		{"string", WithPartition("1"), "1"},
		{"leading zero", WithPartition("001"), "001"},
		{"named int", WithPartition(partitionNumber(-1)), "wrong"},
		{"named string", WithPartition(partitionText("001")), "wrong"},
		{"max uint", WithPartition(^uint64(0)), "18446744073709551615"},
		{"min int", WithPartition(int64(-9223372036854775808)), "-9223372036854775808"},
		{"zero", WithPartition(0), "0"},
		{"empty", WithPartition(""), ""},
		{"unicode", WithPartition("分区.* > /"), "分区.* > /"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p inf.SelectParam
			tt.option(&p)
			if p.Partition == nil || any(*p.Partition) != tt.want {
				t.Fatalf("partition = %v; want %q", p.Partition, tt.want)
			}
		})
	}
}

func TestWithPartitionAnyAndNil(t *testing.T) {
	for _, v := range []any{true, 1.5, []int{1, 2}, struct{ X int }{2}} {
		var p inf.SelectParam
		WithPartition(v)(&p)
		if p.Partition == nil || *p.Partition != fmt.Sprint(v) {
			t.Fatalf("unexpected partition for %T", v)
		}
	}
	for _, v := range []any{nil, (*int)(nil), []int(nil), map[string]int(nil)} {
		var p inf.SelectParam
		WithPartition(1)(&p)
		WithPartition(v)(&p)
		if p.Partition != nil {
			t.Fatal("nil must clear filter")
		}
	}
}

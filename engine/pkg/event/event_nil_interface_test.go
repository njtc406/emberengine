package event

import (
	"reflect"
	"testing"

	"go.etcd.io/etcd/api/v3/mvccpb"
)

func TestEventDataNilInterface(t *testing.T) {
	var pointer *mvccpb.KeyValue
	var dataSlice []byte
	var dataMap map[string]int
	var dataFunc func()
	var dataChan chan int
	for _, tt := range []struct {
		name string
		get  func() any
	}{
		{"generic pointer", (&Event[*mvccpb.KeyValue]{Data: pointer}).GetData},
		{"discovery event", NewDiscoveryEvent().GetData},
		{"interface pointer", (&Event[any]{Data: pointer}).GetData},
		{"nil interface", (&Event[any]{}).GetData},
		{"slice", (&Event[[]byte]{Data: dataSlice}).GetData},
		{"map", (&Event[map[string]int]{Data: dataMap}).GetData},
		{"func", (&Event[func()]{Data: dataFunc}).GetData},
		{"chan", (&Event[chan int]{Data: dataChan}).GetData},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.get(); got != nil {
				t.Fatalf("absent event data = %T, want nil interface", got)
			}
		})
	}
}

func TestEventDataPreservesValues(t *testing.T) {
	pointer := &mvccpb.KeyValue{Key: []byte("key")}
	if got := (&Event[*mvccpb.KeyValue]{Data: pointer}).GetData(); got != pointer {
		t.Fatal("non-nil pointer identity must be preserved")
	}
	for _, value := range []any{0, false, "", []byte{}, map[string]int{}} {
		got := (&Event[any]{Data: value}).GetData()
		if got == nil || !reflect.DeepEqual(got, value) {
			t.Fatalf("data = %#v, want %#v with original type", got, value)
		}
	}
}

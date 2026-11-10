package rpc

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/njtc406/emberengine/v2/engine/pkg/actor"
	"github.com/njtc406/emberengine/v2/engine/pkg/utils/errorx"
)

type nilResultReceiver struct{}

func TestCompileCallFuncNilInterfaces(t *testing.T) {
	owner := &nilResultReceiver{}
	present := &actor.PID{Name: "present"}
	failure := errors.New("failed")
	cases := []struct {
		name    string
		fn      any
		want    any
		wantErr error
	}{
		{name: "no results", fn: func(*nilResultReceiver) {}},
		{name: "nil error only", fn: func(*nilResultReceiver) error { return nil }},
		{name: "nil concrete error", fn: func(*nilResultReceiver) *errorx.Error { return nil }},
		{name: "typed nil error", fn: func(*nilResultReceiver) error {
			var err *errorx.Error
			return err
		}},
		{name: "non-nil error", fn: func(*nilResultReceiver) error { return failure }, wantErr: failure},
		{name: "nil pointer", fn: func(*nilResultReceiver) *actor.PID { return nil }},
		{name: "nil pointer with nil error", fn: func(*nilResultReceiver) (*actor.PID, error) { return nil, nil }},
		{name: "interface with nil pointer", fn: func(*nilResultReceiver) any { return (*actor.PID)(nil) }},
		{name: "nil slice", fn: func(*nilResultReceiver) []byte { return nil }},
		{name: "nil map", fn: func(*nilResultReceiver) map[string]int { return nil }},
		{name: "nil function", fn: func(*nilResultReceiver) func() { return nil }},
		{name: "nil channel", fn: func(*nilResultReceiver) chan int { return nil }},
		{name: "non-nil pointer", fn: func(*nilResultReceiver) *actor.PID { return present }, want: present},
		{name: "empty slice", fn: func(*nilResultReceiver) []byte { return []byte{} }, want: []byte{}},
		{name: "empty map", fn: func(*nilResultReceiver) map[string]int { return map[string]int{} }, want: map[string]int{}},
		{name: "zero number", fn: func(*nilResultReceiver) int { return 0 }, want: 0},
		{name: "false", fn: func(*nilResultReceiver) bool { return false }, want: false},
		{name: "multiple results", fn: func(*nilResultReceiver) (*actor.PID, []byte, int, error) {
			return nil, nil, 7, nil
		}, want: []any{nil, nil, 7}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			fn := reflect.ValueOf(tt.fn)
			var outs []reflect.Type
			businessResults := 0
			for i := 0; i < fn.Type().NumOut(); i++ {
				typ := fn.Type().Out(i)
				outs = append(outs, typ)
				if !typ.Implements(reflect.TypeOf((*error)(nil)).Elem()) {
					businessResults++
				}
			}
			call := compileCallFunc(reflect.ValueOf(owner), tt.name, fn,
				[]reflect.Type{reflect.TypeOf(owner)}, outs, businessResults > 1, false, false, nil)
			got, err := call(context.Background(), nil)
			if err != tt.wantErr {
				t.Fatalf("error type = %T, want %T", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("response = %#v (%T), want %#v (%T)", got, got, tt.want, tt.want)
			}
		})
	}
}

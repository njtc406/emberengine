package actor

import (
	"fmt"
	"reflect"
)

// PartitionString normalizes a partition at API boundaries. Nil values denote
// the empty partition; selectors may use IsNilPartition to preserve wildcards.
// Non-nil values follow fmt.Sprint, including Stringer and Formatter methods.
func PartitionString(value any) string {
	if IsNilPartition(value) {
		return ""
	}
	return fmt.Sprint(value)
}

// IsNilPartition includes typed nil pointers, maps, slices, channels and functions.
func IsNilPartition(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

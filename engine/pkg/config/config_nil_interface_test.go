package config

import "testing"

func TestGetServiceConfNilInterface(t *testing.T) {
	creator := func() any { return "config" }
	c := NewConfig()
	c.serviceConfMap["nil entry"] = nil
	c.RegisterServiceConf(&ServiceConfig{ServiceName: "nil creator"})
	c.RegisterServiceConf(&ServiceConfig{ServiceName: "present", CfgCreator: creator})
	for _, name := range []string{"missing", "nil creator", "nil entry"} {
		t.Run(name, func(t *testing.T) {
			if got := c.GetServiceConf(name); got != nil {
				t.Fatalf("GetServiceConf(%q) = %T, want nil interface", name, got)
			}
		})
	}
	got, ok := c.GetServiceConf("present").(func() any)
	if !ok || got() != "config" {
		t.Fatal("non-nil creator must remain callable")
	}
}

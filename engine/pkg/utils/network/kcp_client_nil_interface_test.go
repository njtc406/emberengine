package network

import "testing"

func TestKCPDialNilInterfaceOnClosedFailedDial(t *testing.T) {
	client := &KCPClient{Addr: "[invalid", closeFlag: true}
	if conn := client.dial(); conn != nil {
		t.Fatalf("failed dial after close = %T, want nil interface", conn)
	}
}

func TestKCPDialPreservesConnection(t *testing.T) {
	for _, closed := range []bool{false, true} {
		client := &KCPClient{Addr: "127.0.0.1:1", closeFlag: closed}
		conn := client.dial()
		if conn == nil {
			t.Fatalf("successful UDP dial returned nil (closed=%v)", closed)
		}
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

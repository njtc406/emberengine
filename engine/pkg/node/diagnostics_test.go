package node

import "testing"

func TestGetRuntimeSnapshotNilNode(t *testing.T) {
	var n *Node
	s := n.GetRuntimeSnapshot()
	if s.NodeUID != "" || s.ClusterMode || s.UptimeSecs != 0 {
		t.Fatalf("unexpected snapshot for nil node: %#v", s)
	}
}

func TestGetRuntimeSnapshotZeroNode(t *testing.T) {
	n := &Node{}
	s := n.GetRuntimeSnapshot()
	if s.NodeUID != "" {
		t.Fatalf("expected empty node uid for zero node, got %q", s.NodeUID)
	}
	if s.ClusterMode {
		t.Fatalf("expected cluster mode false for zero node")
	}
	if s.Service.ServiceCount != 0 {
		t.Fatalf("expected service count 0 for zero node, got %d", s.Service.ServiceCount)
	}
}

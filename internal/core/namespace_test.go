package core

import "testing"

func TestNormalizeNamespace(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", "default"},
		{"  ", "default"},
		{"team_a", "team_a"},
		{"Team A", "team_a"},
		{"my.ns", "my_ns"},
	}
	for _, tt := range tests {
		got := NormalizeNamespace(tt.in)
		if got != tt.want {
			t.Errorf("NormalizeNamespace(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNamespaceFromPathStable(t *testing.T) {
	a := NamespaceFromPath("/tmp/one/beagle.yaml")
	b := NamespaceFromPath("/tmp/one/beagle.yaml")
	c := NamespaceFromPath("/tmp/two/beagle.yaml")
	if a != b {
		t.Fatalf("expected stable namespace %q vs %q", a, b)
	}
	if a == c {
		t.Fatalf("expected distinct namespaces %q vs %q", a, c)
	}
}

func TestNamespaceFromPathFallback(t *testing.T) {
	// A path where the parent dir sanitizes to empty should fall back to "cfg"
	ns := NamespaceFromPath("/beagle.yaml")
	if ns == "" {
		t.Fatal("expected non-empty namespace")
	}
}

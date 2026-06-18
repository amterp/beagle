package core

import "testing"

func TestBuildLabel(t *testing.T) {
	got := BuildLabel("alice", "worker")
	want := "com.beagle.alice.worker"
	if got != want {
		t.Fatalf("BuildLabel = %q, want %q", got, want)
	}
}

func TestSanitizeLabelPart(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"Alice", "alice"},
		{"some user", "some_user"},
		{"user.name", "user_name"},
		{"A.B C", "a_b_c"},
	}
	for _, tt := range tests {
		got := SanitizeLabelPart(tt.in)
		if got != tt.want {
			t.Errorf("SanitizeLabelPart(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestManagedGlob(t *testing.T) {
	got := ManagedGlob("/home/alice", "alice")
	want := "/home/alice/Library/LaunchAgents/com.beagle.alice.*.plist"
	if got != want {
		t.Fatalf("ManagedGlob = %q, want %q", got, want)
	}
}

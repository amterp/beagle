package core

import "testing"

func TestCurrentUserWithHome(t *testing.T) {
	ctx, err := CurrentUserWithHome("/tmp/fakehome")
	if err != nil {
		t.Fatal(err)
	}
	if ctx.HomeDir != "/tmp/fakehome" {
		t.Fatalf("expected overridden home, got %q", ctx.HomeDir)
	}
	if ctx.Username == "" {
		t.Fatal("expected non-empty username")
	}
	if ctx.UID == "" {
		t.Fatal("expected non-empty UID")
	}
}

package profile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegisterAndUseProfile(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "beagle.yaml")
	if err := os.WriteFile(cfg, []byte("version: 1\njobs:\n  a:\n    type: service\n    command: [\"/bin/echo\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := Registry{Profiles: map[string]Entry{}}
	entry, err := Register(&r, "team_a", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Namespace != "team_a" {
		t.Fatalf("unexpected namespace: %+v", entry)
	}
	if r.Active != "team_a" {
		t.Fatalf("expected active profile to be set: %+v", r)
	}

	if _, err := Use(&r, "team_a"); err != nil {
		t.Fatal(err)
	}
}

func TestRegisterRejectsNamespaceCollision(t *testing.T) {
	dir := t.TempDir()
	cfg1 := filepath.Join(dir, "a.yaml")
	cfg2 := filepath.Join(dir, "b.yaml")
	if err := os.WriteFile(cfg1, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg2, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := Registry{Profiles: map[string]Entry{}}

	// Register first profile with name "team_a" -> namespace "team_a"
	if _, err := Register(&r, "team_a", cfg1); err != nil {
		t.Fatal(err)
	}

	// Try to register with the same name (should fail as "already exists")
	_, err := Register(&r, "team_a", cfg2)
	if err == nil {
		t.Fatal("expected error for duplicate name")
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

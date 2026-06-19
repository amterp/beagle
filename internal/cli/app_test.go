package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestRunValidateSuccess(t *testing.T) {
	yaml := `version: 1
jobs:
  worker_a:
    type: service
    command: ["/bin/echo", "hello"]
`
	dir := t.TempDir()
	cfg := dir + "/beagle.yaml"
	if err := os.WriteFile(cfg, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	if err := app.Run([]string{"validate", "--config", cfg}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out.String(), "config valid") {
		t.Fatalf("expected valid-config output, got: %s", out.String())
	}
}

func TestRunValidateFailure(t *testing.T) {
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	err := app.Run([]string{"validate", "--config", "/nope/beagle.yaml"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "validation failed:") {
		t.Fatalf("unexpected error: %v", err)
	}
}

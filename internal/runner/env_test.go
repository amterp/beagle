package runner

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestEnvIntValidValue(t *testing.T) {
	os.Setenv("TEST_INT", "42")
	defer os.Unsetenv("TEST_INT")

	var buf bytes.Buffer
	got := EnvInt("TEST_INT", &buf)
	if got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
	if buf.Len() != 0 {
		t.Fatalf("unexpected warning: %s", buf.String())
	}
}

func TestEnvIntEmpty(t *testing.T) {
	os.Unsetenv("TEST_INT_EMPTY")
	var buf bytes.Buffer
	got := EnvInt("TEST_INT_EMPTY", &buf)
	if got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
}

func TestEnvIntInvalidLogsWarning(t *testing.T) {
	os.Setenv("TEST_INT_BAD", "notanumber")
	defer os.Unsetenv("TEST_INT_BAD")

	var buf bytes.Buffer
	got := EnvInt("TEST_INT_BAD", &buf)
	if got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
	if !strings.Contains(buf.String(), "warning") {
		t.Fatalf("expected warning message, got: %s", buf.String())
	}
}

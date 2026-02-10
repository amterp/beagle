package runner

import (
	"os/exec"
	"testing"
)

func TestForwardSignalsCleanup(t *testing.T) {
	// Verify that the stop function doesn't panic or block.
	cmd := exec.Command("/bin/echo", "test")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	stop := ForwardSignals(cmd)
	_ = cmd.Wait()
	stop() // Should not block or panic
}

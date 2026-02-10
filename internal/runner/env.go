package runner

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/amterp/beagle/internal/runlog"
)

// EnvInt reads an integer from an environment variable. Returns 0 if unset.
// Logs a warning to stderr if the value is present but not a valid integer.
func EnvInt(key string, stderr io.Writer) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		fmt.Fprintf(stderr, "beagle-run: warning: %s=%q is not a valid integer\n", key, raw)
		return 0
	}
	return n
}

// BreakerPolicyFromEnv reads circuit breaker configuration from environment
// variables set by the beagle plist.
func BreakerPolicyFromEnv(stderr io.Writer) runlog.BreakerPolicy {
	return runlog.BreakerPolicy{
		MaxFailures:     EnvInt("BEAGLE_BREAKER_MAX_FAILURES", stderr),
		WindowSeconds:   EnvInt("BEAGLE_BREAKER_WINDOW_SECONDS", stderr),
		CooldownSeconds: EnvInt("BEAGLE_BREAKER_COOLDOWN_SECONDS", stderr),
	}
}

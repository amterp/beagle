package config

import (
	"strings"
	"testing"
)

func TestValidateValidFile(t *testing.T) {
	f := File{
		Version: CurrentVersion,
		Defaults: Defaults{
			WorkingDir:      "/tmp",
			Timezone:        "America/Chicago",
			ThrottleSeconds: 30,
			CircuitBreaker: CircuitBreaker{
				MaxFailures:     5,
				WindowSeconds:   600,
				CooldownSeconds: 1800,
			},
		},
		Jobs: Jobs{
			"monthly_report": {
				Type:    "schedule",
				Command: []string{"/usr/local/bin/report", "--send"},
				Restart: "never",
				Schedule: Schedule{
					Cron:     "0 5 1 * *",
					Timezone: "America/Chicago",
				},
			},
			"worker_a": {
				Type:    "service",
				Command: []string{"/usr/local/bin/worker"},
				Restart: "on-failure",
			},
		},
	}

	if err := Validate(f); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
}

func TestParseCatchUp(t *testing.T) {
	ok := map[string]string{"": "inherit/unset", "none": "strict", "6h": "window", "90m": "window", "168h": "max"}
	for in := range ok {
		if _, err := ParseCatchUp(in); err != nil {
			t.Errorf("ParseCatchUp(%q) unexpected error: %v", in, err)
		}
	}
	bad := []string{"1d", "garbage", "200h", "0h", "-3h"}
	for _, in := range bad {
		if _, err := ParseCatchUp(in); err == nil {
			t.Errorf("ParseCatchUp(%q) expected error, got nil", in)
		}
	}
}

func TestResolveCatchUpJobOverridesDefault(t *testing.T) {
	f := File{
		Version:  CurrentVersion,
		Defaults: Defaults{CatchUp: "6h"},
		Jobs: Jobs{
			"inherits": {Type: "service", Command: []string{"/bin/a"}},
			"strict":   {Type: "service", Command: []string{"/bin/b"}, CatchUp: "none"},
			"own":      {Type: "service", Command: []string{"/bin/c"}, CatchUp: "30m"},
		},
	}
	jobs, err := Resolve(f)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"inherits": "6h0m0s", "strict": "0s", "own": "30m0s"}
	for _, j := range jobs {
		if got := j.CatchUp.String(); got != want[j.ID] {
			t.Errorf("job %s catch_up = %s, want %s", j.ID, got, want[j.ID])
		}
	}
}

func TestValidateRejectsInvalidConfig(t *testing.T) {
	f := File{
		Version: 2,
		Jobs: Jobs{
			"Bad": {
				Type:    "timer",
				Command: []string{"report"},
				Schedule: Schedule{
					Cron: "0 5",
				},
			},
		},
	}

	if err := Validate(f); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateSingleCharJobID(t *testing.T) {
	f := File{
		Version: CurrentVersion,
		Jobs: Jobs{
			"a": {
				Type:    "service",
				Command: []string{"/bin/echo"},
			},
		},
	}
	if err := Validate(f); err != nil {
		t.Fatalf("single-char job ID should be valid: %v", err)
	}
}

func TestResolvePartialBreakerNoDefaultsRejected(t *testing.T) {
	// A job with only some breaker fields and no defaults to fill the gaps
	// should be rejected during resolution.
	f := File{
		Version: CurrentVersion,
		Jobs: Jobs{
			"worker_a": {
				Type:    "service",
				Command: []string{"/bin/echo"},
				CircuitBreaker: CircuitBreaker{
					MaxFailures: 5,
					// WindowSeconds and CooldownSeconds missing, no defaults
				},
			},
		},
	}
	_, err := Resolve(f)
	if err == nil {
		t.Fatal("expected error for partial breaker with no defaults")
	}
	if !strings.Contains(err.Error(), "all three") {
		t.Fatalf("expected all-or-nothing error, got: %v", err)
	}
}

func TestResolvePartialBreakerWithDefaultsPasses(t *testing.T) {
	// A job can set some breaker fields and inherit the rest from defaults.
	f := File{
		Version: CurrentVersion,
		Defaults: Defaults{
			CircuitBreaker: CircuitBreaker{
				MaxFailures:     3,
				WindowSeconds:   300,
				CooldownSeconds: 900,
			},
		},
		Jobs: Jobs{
			"worker_a": {
				Type:    "service",
				Command: []string{"/bin/echo"},
				CircuitBreaker: CircuitBreaker{
					MaxFailures: 10,
					// Inherits WindowSeconds and CooldownSeconds from defaults
				},
			},
		},
	}
	jobs, err := Resolve(f)
	if err != nil {
		t.Fatalf("partial breaker + defaults should resolve: %v", err)
	}
	if jobs[0].CircuitBreaker.MaxFailures != 10 {
		t.Fatalf("expected max_failures=10, got %d", jobs[0].CircuitBreaker.MaxFailures)
	}
	if jobs[0].CircuitBreaker.WindowSeconds != 300 {
		t.Fatalf("expected window_seconds=300, got %d", jobs[0].CircuitBreaker.WindowSeconds)
	}
}

func TestResolveFullBreakerPasses(t *testing.T) {
	f := File{
		Version: CurrentVersion,
		Jobs: Jobs{
			"worker_a": {
				Type:    "service",
				Command: []string{"/bin/echo"},
				CircuitBreaker: CircuitBreaker{
					MaxFailures:     5,
					WindowSeconds:   600,
					CooldownSeconds: 1800,
				},
			},
		},
	}
	if _, err := Resolve(f); err != nil {
		t.Fatalf("full breaker should resolve: %v", err)
	}
}

func TestValidateEmptyCommandZeroRejected(t *testing.T) {
	f := File{
		Version: CurrentVersion,
		Jobs: Jobs{
			"worker_a": {
				Type:    "service",
				Command: []string{"  "},
			},
		},
	}
	err := Validate(f)
	if err == nil {
		t.Fatal("expected validation error for empty command[0]")
	}
	if !strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("expected empty command error, got: %v", err)
	}
}

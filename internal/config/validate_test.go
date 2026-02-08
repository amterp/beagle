package config

import "testing"

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

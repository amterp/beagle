package config

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

func Resolve(f File) ([]ResolvedJob, error) {
	if err := Validate(f); err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(f.Jobs))
	for id := range f.Jobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	jobs := make([]ResolvedJob, 0, len(ids))
	for _, id := range ids {
		job := f.Jobs[id]
		restart := strings.TrimSpace(strings.ToLower(job.Restart))
		if restart == "" {
			restart = "never"
		}

		enabled := true
		if job.Enabled != nil {
			enabled = *job.Enabled
		}

		workingDir := job.WorkingDir
		if workingDir == "" {
			workingDir = f.Defaults.WorkingDir
		}

		tz := job.Schedule.Timezone
		if tz == "" {
			tz = f.Defaults.Timezone
		}

		// NOTE: ThrottleSeconds and CircuitBreaker fields use 0 as "unset",
		// which means a user cannot explicitly override a default to 0.
		// Migrating to *int pointers would fix this but is a larger change
		// that should be done separately.
		throttleSeconds := job.ThrottleSeconds
		if throttleSeconds == 0 {
			throttleSeconds = f.Defaults.ThrottleSeconds
		}

		// "" inherits the default; "none" is an explicit strict override.
		catchUpRaw := job.CatchUp
		if catchUpRaw == "" {
			catchUpRaw = f.Defaults.CatchUp
		}
		catchUp, err := ParseCatchUp(catchUpRaw)
		if err != nil {
			return nil, fmt.Errorf("jobs.%s.%w", id, err)
		}

		breaker := job.CircuitBreaker
		if breaker.MaxFailures == 0 {
			breaker.MaxFailures = f.Defaults.CircuitBreaker.MaxFailures
		}
		if breaker.WindowSeconds == 0 {
			breaker.WindowSeconds = f.Defaults.CircuitBreaker.WindowSeconds
		}
		if breaker.CooldownSeconds == 0 {
			breaker.CooldownSeconds = f.Defaults.CircuitBreaker.CooldownSeconds
		}

		// All-or-nothing check on the merged breaker config. This runs after
		// defaults are merged so that a job can set e.g. max_failures and
		// inherit window_seconds + cooldown_seconds from defaults.
		anyBreakerSet := breaker.MaxFailures > 0 || breaker.WindowSeconds > 0 || breaker.CooldownSeconds > 0
		allBreakerSet := breaker.MaxFailures > 0 && breaker.WindowSeconds > 0 && breaker.CooldownSeconds > 0
		if anyBreakerSet && !allBreakerSet {
			return nil, fmt.Errorf("jobs.%s.circuit_breaker: if any field is set, all three (max_failures, window_seconds, cooldown_seconds) must be positive", id)
		}

		env := map[string]string{}
		for k, v := range f.Defaults.Env {
			env[k] = v
		}
		for k, v := range job.Env {
			env[k] = v
		}

		resolved := ResolvedJob{
			ID:         id,
			Type:       strings.ToLower(job.Type),
			Command:    append([]string(nil), job.Command...),
			WorkingDir: workingDir,
			Env:        env,
			Enabled:    enabled,
			Restart:    restart,
			Schedule: Schedule{
				Cron:     job.Schedule.Cron,
				Timezone: tz,
			},
			Throttle:       time.Duration(throttleSeconds) * time.Second,
			CatchUp:        catchUp,
			CircuitBreaker: breaker,
		}

		jobs = append(jobs, resolved)
	}

	return jobs, nil
}

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

		throttleSeconds := job.ThrottleSeconds
		if throttleSeconds == 0 {
			throttleSeconds = f.Defaults.ThrottleSeconds
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
			CircuitBreaker: breaker,
		}

		if resolved.Type == "schedule" && strings.TrimSpace(resolved.Schedule.Cron) == "" {
			return nil, fmt.Errorf("job %s is schedule without cron", id)
		}

		jobs = append(jobs, resolved)
	}

	return jobs, nil
}

package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	jobIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,63}$`)
	cronPattern  = regexp.MustCompile(`^\S+\s+\S+\s+\S+\s+\S+\s+\S+$`)
)

func Validate(f File) error {
	var errs []string

	if f.Version != CurrentVersion {
		errs = append(errs, fmt.Sprintf("version must be %d", CurrentVersion))
	}

	if f.Jobs == nil || len(f.Jobs) == 0 {
		errs = append(errs, "jobs must contain at least one job")
	}

	if f.Defaults.WorkingDir != "" && !filepath.IsAbs(f.Defaults.WorkingDir) {
		errs = append(errs, "defaults.working_dir must be an absolute path")
	}

	if f.Defaults.Timezone != "" {
		if _, err := time.LoadLocation(f.Defaults.Timezone); err != nil {
			errs = append(errs, fmt.Sprintf("defaults.timezone invalid: %v", err))
		}
	}

	if f.Defaults.ThrottleSeconds < 0 {
		errs = append(errs, "defaults.throttle_seconds must be >= 0")
	}

	validateBreaker("defaults.circuit_breaker", f.Defaults.CircuitBreaker, &errs)

	for id, job := range f.Jobs {
		if !jobIDPattern.MatchString(id) {
			errs = append(errs, fmt.Sprintf("jobs.%s: invalid job id", id))
		}

		t := strings.ToLower(strings.TrimSpace(job.Type))
		if t != "service" && t != "schedule" {
			errs = append(errs, fmt.Sprintf("jobs.%s.type must be service or schedule", id))
		}

		if len(job.Command) == 0 {
			errs = append(errs, fmt.Sprintf("jobs.%s.command must be non-empty", id))
		} else if !filepath.IsAbs(job.Command[0]) {
			errs = append(errs, fmt.Sprintf("jobs.%s.command[0] must be an absolute path", id))
		}

		if job.WorkingDir != "" && !filepath.IsAbs(job.WorkingDir) {
			errs = append(errs, fmt.Sprintf("jobs.%s.working_dir must be an absolute path", id))
		}

		restart := strings.ToLower(strings.TrimSpace(job.Restart))
		if restart != "" && restart != "never" && restart != "on-failure" && restart != "always" {
			errs = append(errs, fmt.Sprintf("jobs.%s.restart must be never, on-failure, or always", id))
		}

		if job.ThrottleSeconds < 0 {
			errs = append(errs, fmt.Sprintf("jobs.%s.throttle_seconds must be >= 0", id))
		}

		validateBreaker("jobs."+id+".circuit_breaker", job.CircuitBreaker, &errs)

		scheduleCron := strings.TrimSpace(job.Schedule.Cron)
		if t == "schedule" {
			if scheduleCron == "" {
				errs = append(errs, fmt.Sprintf("jobs.%s.schedule.cron is required for schedule jobs", id))
			} else if !cronPattern.MatchString(scheduleCron) {
				errs = append(errs, fmt.Sprintf("jobs.%s.schedule.cron must have 5 fields", id))
			}
		}
		if t == "service" && scheduleCron != "" {
			errs = append(errs, fmt.Sprintf("jobs.%s.schedule.cron is not allowed for service jobs", id))
		}

		if job.Schedule.Timezone != "" {
			if _, err := time.LoadLocation(job.Schedule.Timezone); err != nil {
				errs = append(errs, fmt.Sprintf("jobs.%s.schedule.timezone invalid: %v", id, err))
			}
		}
	}

	if len(errs) == 0 {
		return nil
	}

	sort.Strings(errs)
	return fmt.Errorf("config validation failed:\n- %s", strings.Join(errs, "\n- "))
}

func validateBreaker(prefix string, b CircuitBreaker, errs *[]string) {
	if b.MaxFailures < 0 {
		*errs = append(*errs, prefix+".max_failures must be >= 0")
	}
	if b.WindowSeconds < 0 {
		*errs = append(*errs, prefix+".window_seconds must be >= 0")
	}
	if b.CooldownSeconds < 0 {
		*errs = append(*errs, prefix+".cooldown_seconds must be >= 0")
	}
}

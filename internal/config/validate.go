package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/amterp/beagle/internal/core"
)

var (
	// Job IDs: 1-64 chars. First char [a-z0-9], rest [a-z0-9_-].
	// The tail quantifier is {0,63} (not {1,63}) to allow single-char IDs.
	jobIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
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

	if _, err := ParseCatchUp(f.Defaults.CatchUp); err != nil {
		errs = append(errs, "defaults."+err.Error())
	}

	validateBreaker("defaults.circuit_breaker", f.Defaults.CircuitBreaker, &errs)

	for id, job := range f.Jobs {
		if !jobIDPattern.MatchString(id) {
			errs = append(errs, fmt.Sprintf("jobs.%s: invalid job id", id))
		}
		if id == core.SupervisorName {
			errs = append(errs, fmt.Sprintf("jobs.%s: %q is reserved for beagle's scheduler", id, core.SupervisorName))
		}

		t := strings.ToLower(strings.TrimSpace(job.Type))
		if t != "service" && t != "schedule" {
			errs = append(errs, fmt.Sprintf("jobs.%s.type must be service or schedule", id))
		}

		if len(job.Command) == 0 {
			errs = append(errs, fmt.Sprintf("jobs.%s.command must be non-empty", id))
		} else if strings.TrimSpace(job.Command[0]) == "" {
			errs = append(errs, fmt.Sprintf("jobs.%s.command[0] must not be empty", id))
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

		if _, err := ParseCatchUp(job.CatchUp); err != nil {
			errs = append(errs, fmt.Sprintf("jobs.%s.%v", id, err))
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

// maxCatchUp bounds how late a missed schedule job may run. It also caps how
// far back the supervisor scans for a missed occurrence.
const maxCatchUp = 168 * time.Hour // 7 days

// ParseCatchUp interprets a catch_up value. Empty means "inherit"; "none"
// (and 0) mean strict - only fire at the scheduled minute. Otherwise it must be
// a positive Go duration (e.g. 6h, 90m) no larger than maxCatchUp. Note that Go
// durations do not accept a "d" (days) unit, so 7 days is written as 168h.
func ParseCatchUp(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || s == "none" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid catch_up %q (use none or a duration like 6h or 90m; days like 1d are not supported - use 24h)", s)
	}
	if d <= 0 {
		return 0, fmt.Errorf("catch_up %q must be positive or none", s)
	}
	if d > maxCatchUp {
		return 0, fmt.Errorf("catch_up %s exceeds the maximum of %s", d, maxCatchUp)
	}
	return d, nil
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
	// Note: the all-or-nothing semantic check (if any field is set, all three
	// must be positive) runs in Resolve() after defaults are merged, so that
	// partial job-level overrides can inherit remaining fields from defaults.
}

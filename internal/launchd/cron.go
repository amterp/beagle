package launchd

import (
	"fmt"
	"strconv"
	"strings"
)

type Calendar struct {
	Minute  *int
	Hour    *int
	Day     *int
	Month   *int
	Weekday *int
}

// ParseCron parses a 5-field cron expression and returns one or more Calendar
// entries. Supports: *, N, N-M, */N, N-M/S, and comma-separated lists where
// each element can be any of the above forms.
//
// Multiple values in a field produce multiple Calendar entries via cartesian
// product across all fields.
func ParseCron(cron string) ([]Calendar, error) {
	parts := strings.Fields(strings.TrimSpace(cron))
	if len(parts) != 5 {
		return nil, fmt.Errorf("cron must have 5 fields")
	}

	type fieldDef struct {
		name     string
		min, max int
	}
	fields := []fieldDef{
		{"minute", 0, 59},
		{"hour", 0, 23},
		{"day", 1, 31},
		{"month", 1, 12},
		{"weekday", 0, 6},
	}

	// Parse each field into a slice of values. nil means wildcard.
	parsed := make([][]*int, 5)
	for i, fd := range fields {
		vals, err := parseField(parts[i], fd.min, fd.max)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", fd.name, err)
		}
		parsed[i] = vals
	}

	// Build calendars via cartesian product of all field values.
	calendars := []Calendar{{}}
	for fieldIdx := 0; fieldIdx < 5; fieldIdx++ {
		vals := parsed[fieldIdx]
		var next []Calendar
		for _, cal := range calendars {
			for _, v := range vals {
				c := cal
				switch fieldIdx {
				case 0:
					c.Minute = v
				case 1:
					c.Hour = v
				case 2:
					c.Day = v
				case 3:
					c.Month = v
				case 4:
					c.Weekday = v
				}
				next = append(next, c)
			}
		}
		calendars = next
	}

	// launchd StartCalendarInterval doesn't support ranges or steps natively,
	// so we expand them into individual entries. Cap the total to prevent
	// bloated plists that are hard to debug and may hit undocumented limits.
	const maxEntries = 256
	if len(calendars) > maxEntries {
		return nil, fmt.Errorf("cron %q expands to %d calendar entries (max %d) - simplify the expression or split into multiple jobs", cron, len(calendars), maxEntries)
	}

	return calendars, nil
}

// parseField parses a single cron field and returns a slice of pointer-to-int
// values. A nil pointer represents wildcard (*). Each non-nil pointer is a
// specific value that matched.
func parseField(s string, min, max int) ([]*int, error) {
	// Handle comma-separated lists: each element can be *, N, N-M, */N, N-M/S
	if strings.Contains(s, ",") {
		var all []*int
		for _, part := range strings.Split(s, ",") {
			vals, err := parseSingleOrRange(strings.TrimSpace(part), min, max)
			if err != nil {
				return nil, err
			}
			all = append(all, vals...)
		}
		return all, nil
	}
	return parseSingleOrRange(s, min, max)
}

func parseSingleOrRange(s string, min, max int) ([]*int, error) {
	// Wildcard
	if s == "*" {
		return []*int{nil}, nil
	}

	// Step: */N or N-M/S
	if strings.Contains(s, "/") {
		return parseStep(s, min, max)
	}

	// Range: N-M
	if strings.Contains(s, "-") {
		return parseRange(s, min, max)
	}

	// Single value
	v, err := strconv.Atoi(s)
	if err != nil {
		return nil, fmt.Errorf("expected integer, got %q", s)
	}
	if v < min || v > max {
		return nil, fmt.Errorf("%d out of range [%d,%d]", v, min, max)
	}
	return []*int{intPtr(v)}, nil
}

func parseStep(s string, min, max int) ([]*int, error) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid step: %q", s)
	}
	step, err := strconv.Atoi(parts[1])
	if err != nil || step <= 0 {
		return nil, fmt.Errorf("step must be a positive integer: %q", parts[1])
	}

	rangeStart, rangeEnd := min, max
	if parts[0] != "*" {
		// N-M/S form
		if strings.Contains(parts[0], "-") {
			rangeParts := strings.SplitN(parts[0], "-", 2)
			rangeStart, err = strconv.Atoi(rangeParts[0])
			if err != nil {
				return nil, fmt.Errorf("invalid range start: %q", rangeParts[0])
			}
			rangeEnd, err = strconv.Atoi(rangeParts[1])
			if err != nil {
				return nil, fmt.Errorf("invalid range end: %q", rangeParts[1])
			}
		} else {
			rangeStart, err = strconv.Atoi(parts[0])
			if err != nil {
				return nil, fmt.Errorf("invalid step base: %q", parts[0])
			}
		}
	}

	if rangeStart < min || rangeEnd > max || rangeStart > rangeEnd {
		return nil, fmt.Errorf("range [%d,%d] out of bounds [%d,%d]", rangeStart, rangeEnd, min, max)
	}

	var vals []*int
	for v := rangeStart; v <= rangeEnd; v += step {
		vals = append(vals, intPtr(v))
	}
	return vals, nil
}

func parseRange(s string, min, max int) ([]*int, error) {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid range: %q", s)
	}
	start, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid range start: %q", parts[0])
	}
	end, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid range end: %q", parts[1])
	}
	if start < min || end > max || start > end {
		return nil, fmt.Errorf("range [%d,%d] out of bounds [%d,%d]", start, end, min, max)
	}
	var vals []*int
	for v := start; v <= end; v++ {
		vals = append(vals, intPtr(v))
	}
	return vals, nil
}

func intPtr(v int) *int {
	return &v
}

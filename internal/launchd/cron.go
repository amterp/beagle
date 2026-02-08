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

func ParseCron(cron string) (Calendar, error) {
	parts := strings.Fields(strings.TrimSpace(cron))
	if len(parts) != 5 {
		return Calendar{}, fmt.Errorf("cron must have 5 fields")
	}

	minute, err := parsePart(parts[0], 0, 59)
	if err != nil {
		return Calendar{}, fmt.Errorf("minute: %w", err)
	}
	hour, err := parsePart(parts[1], 0, 23)
	if err != nil {
		return Calendar{}, fmt.Errorf("hour: %w", err)
	}
	day, err := parsePart(parts[2], 1, 31)
	if err != nil {
		return Calendar{}, fmt.Errorf("day: %w", err)
	}
	month, err := parsePart(parts[3], 1, 12)
	if err != nil {
		return Calendar{}, fmt.Errorf("month: %w", err)
	}
	weekday, err := parsePart(parts[4], 0, 6)
	if err != nil {
		return Calendar{}, fmt.Errorf("weekday: %w", err)
	}

	return Calendar{Minute: minute, Hour: hour, Day: day, Month: month, Weekday: weekday}, nil
}

func parsePart(s string, min int, max int) (*int, error) {
	if s == "*" {
		return nil, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return nil, fmt.Errorf("expected '*' or integer")
	}
	if v < min || v > max {
		return nil, fmt.Errorf("must be in range [%d,%d]", min, max)
	}
	return &v, nil
}

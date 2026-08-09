package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LocalZone is the timezone value meaning "whatever zone this machine is in
// right now", as opposed to a fixed IANA name. A job set to it moves with the
// machine: fly to another continent and its 07:00 stays 07:00 where you are.
const LocalZone = "local"

// zoneinfoMarker is the path segment that precedes the IANA name in the
// symlink /etc/localtime points at, on both macOS
// (/var/db/timezone/zoneinfo/Europe/Lisbon) and Linux
// (/usr/share/zoneinfo/Europe/Lisbon).
const zoneinfoMarker = "zoneinfo/"

// LoadZone resolves a configured timezone to a location and the name to
// compare and display it by. The name matters as much as the location:
// detecting that the machine moved means comparing today's zone against the
// one recorded at the last fire, and time.Local reports itself as "Local"
// everywhere, which would make every zone look identical to every other.
//
// An empty tz means UTC, not machine-local. That predates LocalZone and is
// kept for compatibility - changing it would silently reschedule every job
// that omits the field.
func LoadZone(tz string) (*time.Location, string, error) {
	if strings.EqualFold(tz, LocalZone) {
		loc, name := systemZone()
		return loc, name, nil
	}
	if tz == "" {
		return time.UTC, "UTC", nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, "", err
	}
	return loc, tz, nil
}

// IsLocalZone reports whether a configured timezone value asks to follow the
// machine.
func IsLocalZone(tz string) bool {
	return strings.EqualFold(tz, LocalZone)
}

// MachineZone is the IANA name of the zone this machine is in. It cannot fail -
// the fallbacks in systemZone always yield something - so callers that only
// need the name for display or comparison use this rather than handling an
// error that never arrives.
func MachineZone() string {
	_, name := systemZone()
	return name
}

// systemZone returns the machine's current zone plus its IANA name.
//
// It does not use time.Local, which resolves the right offset but names itself
// "Local". Two different machine zones would then compare equal and a move
// would go undetected, which is exactly the case LocalZone exists to serve.
//
// Reading /etc/localtime afresh is safe here because the supervisor is a
// one-shot process launchd respawns every minute, so there is no long-lived
// cache to invalidate - a machine that changes zone is noticed within a tick.
//
// The fallbacks degrade rather than fail: an unreadable symlink still yields a
// working location, just one whose name cannot detect a move.
func systemZone() (*time.Location, string) {
	if tz := os.Getenv("TZ"); tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc, tz
		}
	}
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if i := strings.Index(target, zoneinfoMarker); i >= 0 {
			name := filepath.Clean(target[i+len(zoneinfoMarker):])
			if loc, err := time.LoadLocation(name); err == nil {
				return loc, name
			}
		}
	}
	return time.Local, time.Local.String()
}

// ZoneLabel is the short form shown in a column: the last path segment of an
// IANA name ("America/Chicago" -> "Chicago"), or "local" for a job that
// follows the machine. Jobs are rarely ambiguous at that granularity and the
// full name costs too much width.
func ZoneLabel(tz string) string {
	if IsLocalZone(tz) {
		return LocalZone
	}
	if tz == "" {
		return "UTC"
	}
	if i := strings.LastIndex(tz, "/"); i >= 0 {
		return tz[i+1:]
	}
	return tz
}

// DescribeZone renders a configured timezone for a detail view, expanding
// LocalZone to the zone it currently resolves to so the reader can see both
// the intent and today's answer.
func DescribeZone(tz string) string {
	if IsLocalZone(tz) {
		_, name := systemZone()
		return fmt.Sprintf("%s (%s)", LocalZone, name)
	}
	if tz == "" {
		return "UTC"
	}
	return tz
}

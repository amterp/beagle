package core

import (
	"fmt"
	"strings"
)

// SupervisorName is the reserved job-id-shaped segment for the supervisor's own
// launchd label. Config validation forbids a job from using it, so its label
// (com.beagle.<user>.supervisor) can never collide with a job's.
const SupervisorName = "supervisor"

// BuildLabel constructs a launchd label: com.beagle.<user>.<job>.
func BuildLabel(username, jobID string) string {
	return fmt.Sprintf("com.beagle.%s.%s", username, jobID)
}

// SupervisorLabel is the launchd label of the supervisor agent.
func SupervisorLabel(username string) string {
	return BuildLabel(username, SupervisorName)
}

// SanitizeLabelPart normalizes a string for use in a launchd label,
// replacing spaces and dots with underscores and lowercasing.
func SanitizeLabelPart(s string) string {
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, ".", "_")
	return strings.ToLower(s)
}

// ManagedGlob returns a glob pattern matching every plist beagle manages for
// a user. This is the whole namespace beagle owns, so reconciliation can
// garbage-collect any managed plist no longer backed by config.
func ManagedGlob(home, username string) string {
	return PlistPath(home, fmt.Sprintf("com.beagle.%s.*", username))
}

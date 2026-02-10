package core

import (
	"fmt"
	"strings"
)

// BuildLabel constructs a launchd label: com.beagle.<user>.<ns>.<job>.
func BuildLabel(username, namespace, jobID string) string {
	return fmt.Sprintf("com.beagle.%s.%s.%s", username, namespace, jobID)
}

// SanitizeLabelPart normalizes a string for use in a launchd label,
// replacing spaces and dots with underscores and lowercasing.
func SanitizeLabelPart(s string) string {
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, ".", "_")
	return strings.ToLower(s)
}

// ManagedGlob returns a glob pattern matching all plists managed by beagle
// for a given user and namespace.
func ManagedGlob(home, username, namespace string) string {
	return PlistPath(home, fmt.Sprintf("com.beagle.%s.%s.*", username, namespace))
}

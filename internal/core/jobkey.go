package core

import "strings"

// BuildJobKey constructs a namespaced job key. If namespace is non-empty,
// the key is "namespace:jobID"; otherwise just "jobID".
func BuildJobKey(namespace, jobID string) string {
	if namespace != "" {
		return namespace + ":" + jobID
	}
	return jobID
}

// SplitJobKey splits a job key into namespace and job ID.
// If the key has no colon, namespace is empty.
func SplitJobKey(key string) (namespace, jobID string) {
	parts := strings.SplitN(key, ":", 2)
	if len(parts) != 2 {
		return "", key
	}
	return parts[0], parts[1]
}

// SplitJobSelector splits a CLI "profile:job" selector into its parts.
// If there's no colon or either part is empty, profile is empty and
// jobID is the full input.
func SplitJobSelector(raw string) (profile, jobID string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", raw
	}
	return strings.TrimSpace(strings.ToLower(parts[0])), parts[1]
}

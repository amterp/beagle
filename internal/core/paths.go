package core

import "path/filepath"

// DataDir returns the base data directory for beagle state.
func DataDir(home string) string {
	return filepath.Join(home, ".local", "share", "beagle")
}

// LogsDir returns the logs root for a given namespace.
func LogsDir(home, namespace string) string {
	return filepath.Join(DataDir(home), "logs", namespace)
}

// JobLogDir returns the log directory for a specific job.
func JobLogDir(home, namespace, jobID string) string {
	return filepath.Join(LogsDir(home, namespace), jobID)
}

// LogFilePath returns the path to a job's stdout or stderr log.
func LogFilePath(home, namespace, jobID, stream string) string {
	return filepath.Join(JobLogDir(home, namespace, jobID), stream+".log")
}

// RunlogDBPath returns the path to the SQLite run log database.
func RunlogDBPath(home string) string {
	return filepath.Join(DataDir(home), "beagle.db")
}

// ProfileRegistryPath returns the path to the profile registry YAML.
func ProfileRegistryPath(home string) string {
	return filepath.Join(home, ".config", "beagle", "profiles.yaml")
}

// LaunchAgentsDir returns the macOS LaunchAgents directory.
func LaunchAgentsDir(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents")
}

// PlistPath returns the full path for a plist file given a label.
func PlistPath(home, label string) string {
	return filepath.Join(LaunchAgentsDir(home), label+".plist")
}

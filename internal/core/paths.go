package core

import "path/filepath"

// BeagleDir returns beagle's single home directory, holding the config,
// run-log database, and job logs.
func BeagleDir(home string) string {
	return filepath.Join(home, ".beagle")
}

// ConfigPath returns the path to the global jobs config.
func ConfigPath(home string) string {
	return filepath.Join(BeagleDir(home), "jobs.yaml")
}

// LogsDir returns the logs root.
func LogsDir(home string) string {
	return filepath.Join(BeagleDir(home), "logs")
}

// JobLogDir returns the log directory for a specific job.
func JobLogDir(home, jobID string) string {
	return filepath.Join(LogsDir(home), jobID)
}

// LogFilePath returns the path to a job's stdout or stderr log.
func LogFilePath(home, jobID, stream string) string {
	return filepath.Join(JobLogDir(home, jobID), stream+".log")
}

// RunlogDBPath returns the path to the SQLite run log database.
func RunlogDBPath(home string) string {
	return filepath.Join(BeagleDir(home), "beagle.db")
}

// LaunchAgentsDir returns the macOS LaunchAgents directory.
func LaunchAgentsDir(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents")
}

// PlistPath returns the full path for a plist file given a label.
func PlistPath(home, label string) string {
	return filepath.Join(LaunchAgentsDir(home), label+".plist")
}

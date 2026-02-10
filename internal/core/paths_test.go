package core

import "testing"

func TestDataDir(t *testing.T) {
	got := DataDir("/home/alice")
	want := "/home/alice/.local/share/beagle"
	if got != want {
		t.Fatalf("DataDir = %q, want %q", got, want)
	}
}

func TestLogsDir(t *testing.T) {
	got := LogsDir("/home/alice", "team-a")
	want := "/home/alice/.local/share/beagle/logs/team-a"
	if got != want {
		t.Fatalf("LogsDir = %q, want %q", got, want)
	}
}

func TestLogFilePath(t *testing.T) {
	got := LogFilePath("/home/alice", "team-a", "worker", "stdout")
	want := "/home/alice/.local/share/beagle/logs/team-a/worker/stdout.log"
	if got != want {
		t.Fatalf("LogFilePath = %q, want %q", got, want)
	}
}

func TestRunlogDBPath(t *testing.T) {
	got := RunlogDBPath("/home/alice")
	want := "/home/alice/.local/share/beagle/beagle.db"
	if got != want {
		t.Fatalf("RunlogDBPath = %q, want %q", got, want)
	}
}

func TestProfileRegistryPath(t *testing.T) {
	got := ProfileRegistryPath("/home/alice")
	want := "/home/alice/.config/beagle/profiles.yaml"
	if got != want {
		t.Fatalf("ProfileRegistryPath = %q, want %q", got, want)
	}
}

func TestLaunchAgentsDir(t *testing.T) {
	got := LaunchAgentsDir("/home/alice")
	want := "/home/alice/Library/LaunchAgents"
	if got != want {
		t.Fatalf("LaunchAgentsDir = %q, want %q", got, want)
	}
}

func TestPlistPath(t *testing.T) {
	got := PlistPath("/home/alice", "com.beagle.alice.default.worker")
	want := "/home/alice/Library/LaunchAgents/com.beagle.alice.default.worker.plist"
	if got != want {
		t.Fatalf("PlistPath = %q, want %q", got, want)
	}
}

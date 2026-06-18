package core

import "testing"

func TestBeagleDir(t *testing.T) {
	got := BeagleDir("/home/alice")
	want := "/home/alice/.beagle"
	if got != want {
		t.Fatalf("BeagleDir = %q, want %q", got, want)
	}
}

func TestConfigPath(t *testing.T) {
	got := ConfigPath("/home/alice")
	want := "/home/alice/.beagle/jobs.yaml"
	if got != want {
		t.Fatalf("ConfigPath = %q, want %q", got, want)
	}
}

func TestLogsDir(t *testing.T) {
	got := LogsDir("/home/alice")
	want := "/home/alice/.beagle/logs"
	if got != want {
		t.Fatalf("LogsDir = %q, want %q", got, want)
	}
}

func TestLogFilePath(t *testing.T) {
	got := LogFilePath("/home/alice", "worker", "stdout")
	want := "/home/alice/.beagle/logs/worker/stdout.log"
	if got != want {
		t.Fatalf("LogFilePath = %q, want %q", got, want)
	}
}

func TestRunlogDBPath(t *testing.T) {
	got := RunlogDBPath("/home/alice")
	want := "/home/alice/.beagle/beagle.db"
	if got != want {
		t.Fatalf("RunlogDBPath = %q, want %q", got, want)
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
	got := PlistPath("/home/alice", "com.beagle.alice.worker")
	want := "/home/alice/Library/LaunchAgents/com.beagle.alice.worker.plist"
	if got != want {
		t.Fatalf("PlistPath = %q, want %q", got, want)
	}
}

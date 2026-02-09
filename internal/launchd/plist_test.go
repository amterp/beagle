package launchd

import (
	"strings"
	"testing"

	"github.com/amterp/beagle/internal/config"
)

func TestBuildAndRenderServicePlist(t *testing.T) {
	rj := config.ResolvedJob{
		ID:      "worker_a",
		Type:    "service",
		Command: []string{"/usr/local/bin/worker"},
		Restart: "on-failure",
		Enabled: true,
	}

	spec, err := BuildSpec("com.beagle.test.worker_a", rj, "beagle-run", "/tmp/stdout.log", "/tmp/stderr.log", "team-a")
	if err != nil {
		t.Fatal(err)
	}

	plist, err := RenderPlist(spec)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(plist, "<key>KeepAlive</key>") {
		t.Fatalf("expected KeepAlive in plist: %s", plist)
	}
	if !strings.Contains(plist, "<key>ProgramArguments</key>") {
		t.Fatalf("expected ProgramArguments in plist: %s", plist)
	}
}

func TestBuildAndRenderSchedulePlist(t *testing.T) {
	rj := config.ResolvedJob{
		ID:      "monthly_report",
		Type:    "schedule",
		Command: []string{"/usr/local/bin/report"},
		Enabled: true,
		Schedule: config.Schedule{
			Cron: "0 5 1 * *",
		},
	}

	spec, err := BuildSpec("com.beagle.test.monthly_report", rj, "beagle-run", "/tmp/stdout.log", "/tmp/stderr.log", "team-a")
	if err != nil {
		t.Fatal(err)
	}

	plist, err := RenderPlist(spec)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(plist, "<key>StartCalendarInterval</key>") {
		t.Fatalf("expected StartCalendarInterval in plist: %s", plist)
	}
	if !strings.Contains(plist, "<key>Hour</key>") || !strings.Contains(plist, "<integer>5</integer>") {
		t.Fatalf("expected hour value in schedule plist: %s", plist)
	}
}

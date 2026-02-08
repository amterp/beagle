package launchd

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/amterp/beagle/internal/config"
)

type JobSpec struct {
	Label       string
	ProgramArgs []string
	WorkingDir  string
	Env         map[string]string
	StdoutPath  string
	StderrPath  string
	Enabled     bool
	Type        string
	Restart     string
	ThrottleSec int
	Calendar    *Calendar
	Timezone    string
}

func BuildSpec(label string, rj config.ResolvedJob, runnerPath string, stdoutPath string, stderrPath string) (JobSpec, error) {
	spec := JobSpec{
		Label:       label,
		ProgramArgs: append([]string{runnerPath, "--job", rj.ID, "--"}, rj.Command...),
		WorkingDir:  rj.WorkingDir,
		Env:         map[string]string{},
		StdoutPath:  stdoutPath,
		StderrPath:  stderrPath,
		Enabled:     rj.Enabled,
		Type:        rj.Type,
		Restart:     rj.Restart,
		ThrottleSec: int(rj.Throttle.Seconds()),
		Timezone:    rj.Schedule.Timezone,
	}
	for k, v := range rj.Env {
		spec.Env[k] = v
	}
	spec.Env["BEAGLE_JOB_ID"] = rj.ID
	spec.Env["BEAGLE_JOB_TYPE"] = rj.Type
	spec.Env["BEAGLE_BREAKER_MAX_FAILURES"] = fmt.Sprintf("%d", rj.CircuitBreaker.MaxFailures)
	spec.Env["BEAGLE_BREAKER_WINDOW_SECONDS"] = fmt.Sprintf("%d", rj.CircuitBreaker.WindowSeconds)
	spec.Env["BEAGLE_BREAKER_COOLDOWN_SECONDS"] = fmt.Sprintf("%d", rj.CircuitBreaker.CooldownSeconds)
	if rj.Type == "schedule" {
		spec.Env["BEAGLE_SCHEDULE_CRON"] = rj.Schedule.Cron
		spec.Env["BEAGLE_SCHEDULE_TIMEZONE"] = rj.Schedule.Timezone
		spec.Env["BEAGLE_SCHEDULE_STRICT_TZ"] = "1"
	}

	if rj.Type == "schedule" {
		cal, err := ParseCron(rj.Schedule.Cron)
		if err != nil {
			return JobSpec{}, fmt.Errorf("parse cron for %s: %w", rj.ID, err)
		}
		spec.Calendar = &cal
	}

	return spec, nil
}

func RenderPlist(spec JobSpec) (string, error) {
	if spec.Label == "" {
		return "", fmt.Errorf("label is required")
	}
	if len(spec.ProgramArgs) == 0 {
		return "", fmt.Errorf("program arguments are required")
	}

	var b bytes.Buffer
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	b.WriteString("<plist version=\"1.0\">\n")
	b.WriteString("<dict>\n")

	writeKeyString(&b, "Label", spec.Label)

	b.WriteString("  <key>ProgramArguments</key>\n")
	b.WriteString("  <array>\n")
	for _, arg := range spec.ProgramArgs {
		b.WriteString("    <string>")
		b.WriteString(escape(arg))
		b.WriteString("</string>\n")
	}
	b.WriteString("  </array>\n")

	if spec.WorkingDir != "" {
		writeKeyString(&b, "WorkingDirectory", spec.WorkingDir)
	}

	if len(spec.Env) > 0 {
		keys := make([]string, 0, len(spec.Env))
		for k := range spec.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteString("  <key>EnvironmentVariables</key>\n")
		b.WriteString("  <dict>\n")
		for _, k := range keys {
			writeDictString(&b, k, spec.Env[k])
		}
		b.WriteString("  </dict>\n")
	}

	writeKeyString(&b, "StandardOutPath", spec.StdoutPath)
	writeKeyString(&b, "StandardErrorPath", spec.StderrPath)

	if spec.ThrottleSec > 0 {
		writeKeyInt(&b, "ThrottleInterval", spec.ThrottleSec)
	}

	if spec.Type == "service" {
		switch spec.Restart {
		case "always", "on-failure":
			writeKeyTrue(&b, "KeepAlive")
		default:
			writeKeyFalse(&b, "KeepAlive")
		}
	}

	if spec.Type == "schedule" && spec.Calendar != nil {
		b.WriteString("  <key>StartCalendarInterval</key>\n")
		b.WriteString("  <dict>\n")
		if spec.Calendar.Minute != nil {
			writeDictInt(&b, "Minute", *spec.Calendar.Minute)
		}
		if spec.Calendar.Hour != nil {
			writeDictInt(&b, "Hour", *spec.Calendar.Hour)
		}
		if spec.Calendar.Day != nil {
			writeDictInt(&b, "Day", *spec.Calendar.Day)
		}
		if spec.Calendar.Month != nil {
			writeDictInt(&b, "Month", *spec.Calendar.Month)
		}
		if spec.Calendar.Weekday != nil {
			writeDictInt(&b, "Weekday", *spec.Calendar.Weekday)
		}
		b.WriteString("  </dict>\n")
	}

	if spec.Timezone != "" {
		writeKeyString(&b, "BeagleTimezone", spec.Timezone)
	}

	if spec.Enabled {
		writeKeyFalse(&b, "Disabled")
	} else {
		writeKeyTrue(&b, "Disabled")
	}

	b.WriteString("</dict>\n")
	b.WriteString("</plist>\n")

	return b.String(), nil
}

func writeKeyString(b *bytes.Buffer, key string, value string) {
	b.WriteString("  <key>")
	b.WriteString(escape(key))
	b.WriteString("</key>\n")
	b.WriteString("  <string>")
	b.WriteString(escape(value))
	b.WriteString("</string>\n")
}

func writeKeyInt(b *bytes.Buffer, key string, value int) {
	b.WriteString("  <key>")
	b.WriteString(escape(key))
	b.WriteString("</key>\n")
	b.WriteString("  <integer>")
	b.WriteString(fmt.Sprintf("%d", value))
	b.WriteString("</integer>\n")
}

func writeKeyTrue(b *bytes.Buffer, key string) {
	b.WriteString("  <key>")
	b.WriteString(escape(key))
	b.WriteString("</key>\n")
	b.WriteString("  <true/>\n")
}

func writeKeyFalse(b *bytes.Buffer, key string) {
	b.WriteString("  <key>")
	b.WriteString(escape(key))
	b.WriteString("</key>\n")
	b.WriteString("  <false/>\n")
}

func writeDictString(b *bytes.Buffer, key string, value string) {
	b.WriteString("    <key>")
	b.WriteString(escape(key))
	b.WriteString("</key>\n")
	b.WriteString("    <string>")
	b.WriteString(escape(value))
	b.WriteString("</string>\n")
}

func writeDictInt(b *bytes.Buffer, key string, value int) {
	b.WriteString("    <key>")
	b.WriteString(escape(key))
	b.WriteString("</key>\n")
	b.WriteString("    <integer>")
	b.WriteString(fmt.Sprintf("%d", value))
	b.WriteString("</integer>\n")
}

func escape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return r.Replace(s)
}

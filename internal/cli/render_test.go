package cli

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/amterp/beagle/internal/launchd"
	"github.com/amterp/beagle/internal/runlog"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Force a no-color profile so render output is deterministic regardless of
// whether the test runs under a TTY.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.Ascii)
	os.Exit(m.Run())
}

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

// plain strips any residual ANSI so assertions test layout/text, not styling.
func plain(s string) string { return ansiRe.ReplaceAllString(s, "") }

// colSplit recovers logical columns: cells are separated by a 2-space gutter,
// while single spaces inside a cell (e.g. "● loaded") are preserved.
var colSplit = regexp.MustCompile(` {2,}`)

func columns(line string) []string {
	return colSplit.Split(strings.TrimRight(line, " "), -1)
}

func TestYesNo(t *testing.T) {
	if got := yesNo(true); got != "yes" {
		t.Errorf("yesNo(true) = %q, want yes", got)
	}
	if got := yesNo(false); got != "no" {
		t.Errorf("yesNo(false) = %q, want no", got)
	}
}

func TestRunOutcome(t *testing.T) {
	cases := []struct {
		name string
		s    runlog.RunSummary
		ok   bool
		want string
	}{
		{"succeeded", runlog.RunSummary{Status: "succeeded"}, true, "✓ ok"},
		{"running", runlog.RunSummary{Status: "running"}, true, "● running"},
		{"failed", runlog.RunSummary{Status: "failed", ExitCode: 2}, true, "✗ exit 2"},
		{"never", runlog.RunSummary{}, false, "· never"},
	}
	for _, c := range cases {
		if got := plain(runOutcome(c.s, c.ok)); got != c.want {
			t.Errorf("%s: runOutcome = %q, want %q", c.name, got, c.want)
		}
	}
	if got := runWhen(runlog.RunSummary{}, false); got != "" {
		t.Errorf("runWhen(_, false) = %q, want empty", got)
	}
}

func TestStateCell(t *testing.T) {
	cases := []struct {
		name string
		st   launchd.JobStatus
		want string
	}{
		{"loaded", launchd.JobStatus{Loaded: true}, "● loaded"},
		{"paused", launchd.JobStatus{Loaded: true, Disabled: true}, "⏸ paused"},
		{"stopped", launchd.JobStatus{}, "○ stopped"},
	}
	for _, c := range cases {
		if got := plain(stateCell(c.st)); got != c.want {
			t.Errorf("%s: stateCell = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCheck(t *testing.T) {
	if got := plain(check(true, "home directory", "")); got != "✓ home directory" {
		t.Errorf("check ok = %q", got)
	}
	if got := plain(check(false, "supervisor ticking", "stale 9m ago")); got != "✗ supervisor ticking   stale 9m ago" {
		t.Errorf("check fail = %q", got)
	}
}

func TestApplyLine(t *testing.T) {
	got := plain(applyLine(launchd.Summary{Created: 1, Unchanged: 3}))
	want := "✓ applied   +1 created  ~0 updated  -0 removed  3 unchanged"
	if got != want {
		t.Errorf("applyLine = %q, want %q", got, want)
	}
	errLine := plain(applyLine(launchd.Summary{Errors: []string{"boom"}}))
	if !strings.HasPrefix(errLine, "✗ applied with errors") {
		t.Errorf("applyLine with errors = %q", errLine)
	}
}

func TestTableAlignsPlain(t *testing.T) {
	got := plain(table([]string{"JOB", "TYPE"}, [][]string{
		{"a", "x"},
		{"bb", "y"},
	}))
	want := "JOB  TYPE\na    x\nbb   y\n"
	if got != want {
		t.Errorf("table layout mismatch:\n got %q\nwant %q", got, want)
	}
}

// Glyph cells have more bytes than display columns; this proves the gutter
// (and thus column alignment) is measured in display width, not bytes - the
// exact failure mode that rules out text/tabwriter here.
func TestTableAlignsGlyphCells(t *testing.T) {
	out := plain(table([]string{"STATE", "WHEN"}, [][]string{
		{"● loaded", "06-18"},
		{"○ stopped", "-"},
	}))
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), out)
	}
	wantRows := [][]string{{"● loaded", "06-18"}, {"○ stopped", "-"}}
	for i, want := range wantRows {
		if got := columns(lines[i+1]); !equalSlices(got, want) {
			t.Errorf("row %d columns = %v, want %v", i, got, want)
		}
	}
}

// An empty trailing column (e.g. ls's blank "WHEN" header) once emitted bold
// on/off codes for the empty cell, leaving trailing spaces stranded before the
// escape sequence. Ascii mode can't reproduce it, so this drives a color
// profile and checks the visible line has no trailing whitespace.
func TestTableNoTrailingWhitespaceUnderColor(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	out := table([]string{"A", ""}, [][]string{{"x", ""}})
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if visible := plain(line); visible != strings.TrimRight(visible, " ") {
			t.Errorf("trailing whitespace after stripping ANSI: %q", line)
		}
	}
}

func TestKVAligns(t *testing.T) {
	got := plain(kv([][2]string{
		{"job", "worker"},
		{"type", "service"},
	}))
	want := "job   worker\ntype  service\n"
	if got != want {
		t.Errorf("kv layout mismatch:\n got %q\nwant %q", got, want)
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

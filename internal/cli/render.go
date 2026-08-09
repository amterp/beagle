package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/amterp/beagle/internal/config"
	"github.com/amterp/beagle/internal/launchd"
	"github.com/amterp/beagle/internal/runlog"
	"github.com/charmbracelet/lipgloss"
)

// Status glyphs. These are plain unicode (not color), so they survive piping
// and stay greppable even when color is stripped.
const (
	glyphOK    = "✓"
	glyphFail  = "✗"
	glyphOn    = "●"
	glyphOff   = "○"
	glyphPause = "⏸"
	glyphNone  = "·"
)

// gutter is the gap between columns in tables and kv blocks.
const gutter = 2

// yesNo renders a bool as a plain yes/no - friendlier than Go's true/false in
// a column where the reader scans for "is this on".
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// table renders rows as left-aligned columns with a styled header row and a
// 2-space gutter, no borders. Column widths are measured with lipgloss.Width
// (which ignores ANSI escapes) so colored cells and glyphs still align.
func table(headers []string, rows [][]string) string {
	cols := len(headers)
	widths := make([]int, cols)
	for i := range headers {
		widths[i] = lipgloss.Width(headers[i])
	}
	for _, row := range rows {
		for i := 0; i < cols && i < len(row); i++ {
			if w := lipgloss.Width(row[i]); w > widths[i] {
				widths[i] = w
			}
		}
	}

	var b strings.Builder
	writeRow := func(cells []string, header bool) {
		var line strings.Builder
		for i := 0; i < cols; i++ {
			cell := ""
			if i < len(cells) {
				cell = cells[i]
			}
			// Skip styling empty cells: a styled "" still emits escape codes,
			// which would sit past the trailing pad and defeat the TrimRight below.
			if header && cell != "" {
				line.WriteString(headerStyle.Render(cell))
			} else {
				line.WriteString(cell)
			}
			if i < cols-1 {
				line.WriteString(strings.Repeat(" ", widths[i]-lipgloss.Width(cell)+gutter))
			}
		}
		// Trim trailing pad so empty final columns don't leave dangling spaces.
		b.WriteString(strings.TrimRight(line.String(), " "))
		b.WriteString("\n")
	}

	writeRow(headers, true)
	for _, row := range rows {
		writeRow(row, false)
	}
	return b.String()
}

// section renders a titled table. Widths are computed per section, so the two
// halves of a list size their columns independently rather than padding to
// each other's widest job name.
func section(title string, headers []string, rows [][]string) string {
	return sectionStyle.Render(title) + "\n" + table(headers, rows)
}

// listSections renders the job list split into services and schedules.
//
// The split exists because the two kinds of job share almost no useful columns:
// a service has a PID and an uptime and no schedule, a schedule job has a cron
// expression and a next fire and no PID. One table for both could only show
// their intersection, which is the least informative view available. Splitting
// lets each half answer the question actually asked of it, and makes the TYPE
// column redundant - the heading says it.
//
// Kept free of I/O so the layout stays under test, following breakerGate.
func listSections(items []launchd.JobStatus, health map[string]runlog.RunSummary, now time.Time, machineZone string) string {
	var services, schedules []launchd.JobStatus
	for _, item := range items {
		if item.Type == "service" {
			services = append(services, item)
		} else {
			schedules = append(schedules, item)
		}
	}

	var out []string
	if len(services) > 0 {
		rows := make([][]string, 0, len(services))
		for _, item := range services {
			summary, ok := health[item.ID]
			rows = append(rows, []string{
				item.ID,
				yesNo(item.Enabled),
				serviceStateCell(item, summary, ok),
				uptimeCell(item, summary, ok, now),
				pidCell(item.PID),
			})
		}
		out = append(out, section("SERVICES", []string{"JOB", "ENABLED", "STATE", "UPTIME", "PID"}, rows))
	}
	if len(schedules) > 0 {
		rows := make([][]string, 0, len(schedules))
		for _, item := range schedules {
			summary, ok := health[item.ID]
			rows = append(rows, []string{
				item.ID,
				yesNo(item.Enabled),
				stateCell(item),
				humanizeCron(item.Schedule.Cron),
				zoneCell(item.Schedule.Timezone, machineZone),
				nextCell(item, now),
				runOutcome(summary, ok),
				runWhen(summary, ok),
				runDuration(summary, ok),
			})
		}
		// The blank headers continue the column to their left: the zone belongs
		// to SCHEDULE, the timestamp and duration to LAST RUN. Naming them would
		// imply four independent facts where there are two.
		out = append(out, section("SCHEDULES",
			[]string{"JOB", "ENABLED", "STATE", "SCHEDULE", "", "NEXT", "LAST RUN", "", ""}, rows))
	}
	return strings.Join(out, "\n")
}

// kv renders aligned "label  value" pairs: dim labels padded to the widest
// label, then the value. Used for the single-job status view.
func kv(pairs [][2]string) string {
	w := 0
	for _, p := range pairs {
		if l := lipgloss.Width(p[0]); l > w {
			w = l
		}
	}
	var b strings.Builder
	for _, p := range pairs {
		b.WriteString(labelStyle.Render(p[0]))
		b.WriteString(strings.Repeat(" ", w-lipgloss.Width(p[0])+gutter))
		b.WriteString(p[1])
		b.WriteString("\n")
	}
	return b.String()
}

// stateCell colors a job's launchd state: loaded (green), paused (yellow), or
// stopped (dim). Disabled wins because a paused job is loaded-but-suppressed.
func stateCell(st launchd.JobStatus) string {
	switch {
	case st.Disabled:
		return warnStyle.Render(glyphPause + " paused")
	case st.Loaded:
		return okStyle.Render(glyphOn + " loaded")
	default:
		return dimStyle.Render(glyphOff + " stopped")
	}
}

// serviceStateCell is stateCell for a service, where "loaded" is not the
// interesting question. A service is supposed to be up, so the state that
// matters is whether a process is actually there - and when one is not, why.
//
// The distinction is invisible to stateCell, which only knows that launchd has
// the agent: a service that crashed past its restart limit and one that is
// serving traffic both read as "loaded". Splitting it here is also what lets
// the services table drop the last-run column without losing the failure, since
// a dead service reports its exit code in place of its state.
func serviceStateCell(st launchd.JobStatus, s runlog.RunSummary, ok bool) string {
	switch {
	case st.Disabled:
		return warnStyle.Render(glyphPause + " paused")
	case !st.Loaded:
		return dimStyle.Render(glyphOff + " stopped")
	case st.PID != 0:
		return okStyle.Render(glyphOn + " running")
	case ok && s.Status != "succeeded" && s.Status != "running":
		return failStyle.Render(fmt.Sprintf("%s exit %d", glyphFail, s.ExitCode))
	default:
		return dimStyle.Render(glyphOff + " not running")
	}
}

// runOutcome colors the last-run result. ok=false means we have no recorded run.
func runOutcome(s runlog.RunSummary, ok bool) string {
	if !ok {
		return dimStyle.Render(glyphNone + " never")
	}
	switch s.Status {
	case "succeeded":
		return okStyle.Render(glyphOK + " ok")
	case "running":
		return runStyle.Render(glyphOn + " running")
	default:
		return failStyle.Render(fmt.Sprintf("%s exit %d", glyphFail, s.ExitCode))
	}
}

// runWhen is the dim timestamp of the last run, blank when there is none.
func runWhen(s runlog.RunSummary, ok bool) string {
	if !ok {
		return ""
	}
	return dimStyle.Render(s.StartedAt.Local().Format("01-02 15:04"))
}

// compactDuration renders a span at two units, largest first, dropping the
// smaller unit when it is zero. Spans in this tool run from a sub-second script
// to a service up for weeks, and two units is enough to judge any of them
// without the column growing to fit the worst case.
func compactDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Second:
		// Sub-second runs are real - a shell one-liner finishes in tens of
		// milliseconds - and rounding them to "0s" would read as "did not run".
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return pair(int(d.Minutes()), int(d.Seconds())%60, "m", "s")
	case d < 24*time.Hour:
		return pair(int(d.Hours()), int(d.Minutes())%60, "h", "m")
	default:
		return pair(int(d.Hours())/24, int(d.Hours())%24, "d", "h")
	}
}

func pair(big, small int, bigUnit, smallUnit string) string {
	if small == 0 {
		return fmt.Sprintf("%d%s", big, bigUnit)
	}
	return fmt.Sprintf("%d%s %d%s", big, bigUnit, small, smallUnit)
}

// weekdayNames is indexed by cron's day-of-week field, 0 = Sunday.
var weekdayNames = [...]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

// humanizeCron renders the common schedule shapes in prose and returns the
// expression verbatim for everything else.
//
// It reads the original text rather than a parsed CronSpec because the parse is
// lossy: `*/15` and `0,15,30,45` produce the same set of minutes, so a spec
// cannot tell you which one the user wrote. Falling back to the raw expression
// is the important half of the contract - a column that guesses wrong about
// when a job runs is worse than one that shows cron.
func humanizeCron(cron string) string {
	f := strings.Fields(strings.TrimSpace(cron))
	if len(f) != 5 {
		return cron
	}
	minField, hourField, dom, month, dow := f[0], f[1], f[2], f[3], f[4]
	if month != "*" {
		return cron
	}

	if hourField == "*" && dom == "*" && dow == "*" {
		if minField == "*" {
			return "every minute"
		}
		if step, ok := everyStep(minField); ok {
			return fmt.Sprintf("every %dm", step)
		}
	}

	minute, minOK := cronNum(minField, 0, 59)
	if !minOK {
		return cron
	}
	if hourField == "*" && dom == "*" && dow == "*" {
		return fmt.Sprintf("hourly at :%02d", minute)
	}
	hour, hourOK := cronNum(hourField, 0, 23)
	if !hourOK {
		return cron
	}
	clock := fmt.Sprintf("%02d:%02d", hour, minute)

	switch {
	case dom == "*" && dow == "*":
		return clock + " daily"
	case dom == "*":
		if day, ok := cronNum(dow, 0, 6); ok {
			return clock + " " + weekdayNames[day]
		}
	case dow == "*":
		if day, ok := cronNum(dom, 1, 31); ok {
			return clock + " on the " + ordinal(day)
		}
	}
	return cron
}

// everyStep matches a bare `*/N` field.
func everyStep(field string) (int, bool) {
	if !strings.HasPrefix(field, "*/") {
		return 0, false
	}
	n, err := strconv.Atoi(field[2:])
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// cronNum matches a field holding exactly one in-range value. Lists, ranges and
// steps deliberately fail here so humanizeCron falls back to the raw text.
func cronNum(field string, min, max int) (int, bool) {
	n, err := strconv.Atoi(field)
	if err != nil || n < min || n > max {
		return 0, false
	}
	return n, true
}

func ordinal(n int) string {
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return strconv.Itoa(n) + suffix
}

// zoneCell names the zone a schedule runs in, and is blank when that is the
// machine's own zone. Most jobs match most of the time, so showing it always
// would spend a column on a value repeated down the page; showing it only when
// it differs makes the divergence the thing that catches the eye. A job
// following the machine is always marked, because that is a property of the
// job, not a coincidence of where the machine currently is.
func zoneCell(tz, machineZone string) string {
	if config.IsLocalZone(tz) {
		return dimStyle.Render(config.LocalZone)
	}
	resolved := tz
	if resolved == "" {
		resolved = "UTC"
	}
	if resolved == machineZone {
		return ""
	}
	return dimStyle.Render(config.ZoneLabel(resolved))
}

// nextFire resolves a schedule job's next occurrence. The third return is a
// rendered cell explaining why there is no time to report, empty when there is
// one. A bad cron surfaces here rather than staying buried until the supervisor
// errors into a log nobody reads: validation only counts five fields, so an
// expression like `0 99 * * *` reaches this point intact.
func nextFire(st launchd.JobStatus, now time.Time) (time.Time, *time.Location, string) {
	spec, err := launchd.ParseSpec(st.Schedule.Cron)
	if err != nil {
		return time.Time{}, nil, failStyle.Render(glyphFail + " bad cron")
	}
	loc, _, err := config.LoadZone(st.Schedule.Timezone)
	if err != nil {
		return time.Time{}, nil, failStyle.Render(glyphFail + " bad zone")
	}
	next, ok := spec.NextFire(now, loc, launchd.NextFireHorizon)
	if !ok {
		return time.Time{}, loc, dimStyle.Render(glyphNone + " never")
	}
	return next, loc, ""
}

// inert reports whether a job's schedule is not actually going to happen,
// because it is paused, disabled in config, or has no launchd agent.
func inert(st launchd.JobStatus) bool {
	return !st.Enabled || st.Disabled || !st.Loaded
}

// nextCell is when the schedule fires next, relative to now. Relative rather
// than absolute on purpose: these jobs run in several zones at once, and "in 8m"
// means the same thing in all of them where "05:15" does not.
//
// A job that cannot fire is dimmed rather than blanked. Paused and disabled jobs
// still have a schedule and hiding it would lose real information, but a bright
// countdown would promise something that is not going to happen.
func nextCell(st launchd.JobStatus, now time.Time) string {
	next, _, problem := nextFire(st, now)
	if problem != "" {
		return problem
	}
	text := "in " + compactDuration(next.Sub(now))
	if inert(st) {
		return dimStyle.Render(text)
	}
	return text
}

// scheduleDetail is the single-job view of a schedule. The list column has to
// pick one of prose, raw expression and zone; here there is room for all three,
// which is what someone asking "why did it fire then" needs in one place.
func scheduleDetail(st launchd.JobStatus) string {
	parts := []string{humanizeCron(st.Schedule.Cron)}
	if parts[0] != st.Schedule.Cron {
		parts = append(parts, dimStyle.Render(st.Schedule.Cron))
	}
	return strings.Join(append(parts, dimStyle.Render(config.DescribeZone(st.Schedule.Timezone))), "  ")
}

// nextDetail is the absolute next fire plus how far off it is.
//
// It leads with the job's own zone, because that is the wall clock the cron
// expression names and the one the reader will edit. When the machine is
// somewhere else it also gives the local time, since the line directly below
// this one reports the last run in machine time and two adjacent timestamps in
// unmarked different zones is precisely the confusion this change exists to
// remove.
func nextDetail(st launchd.JobStatus, now time.Time, machineZone string) string {
	next, loc, problem := nextFire(st, now)
	if problem != "" {
		return problem
	}
	stamp := next.In(loc).Format("2006-01-02 15:04")
	if zone := zoneCell(st.Schedule.Timezone, machineZone); zone != "" {
		stamp += " " + zone + dimStyle.Render(fmt.Sprintf(" (%s local)", next.Local().Format("15:04")))
	}
	if inert(st) {
		stamp = dimStyle.Render(stamp)
	}
	return stamp + "  " + dimStyle.Render("in "+compactDuration(next.Sub(now)))
}

// uptimeCell is how long a service's current process has been up.
//
// It needs both sources to agree, and agree about the same process. The run row
// supplies the start time, but a row only reaches "succeeded" or "failed" if
// the wrapper lived long enough to write it: a SIGKILL, a panic or a power cut
// leaves it saying "running" forever. Requiring launchd to report the very same
// PID is what separates a live service from that residue - otherwise a dead job
// shows a confidently growing uptime, which is worse than showing nothing.
func uptimeCell(st launchd.JobStatus, s runlog.RunSummary, ok bool, now time.Time) string {
	if st.PID == 0 || !ok || s.Status != "running" || s.PID != st.PID {
		return dimStyle.Render("-")
	}
	return compactDuration(now.Sub(s.StartedAt))
}

func pidCell(pid int) string {
	if pid == 0 {
		return dimStyle.Render("-")
	}
	return dimStyle.Render(strconv.Itoa(pid))
}

// runDuration is how long the last completed run took, blank while it is still
// going or when nothing was recorded. It makes a job that is quietly getting
// slower visible without opening the logs.
func runDuration(s runlog.RunSummary, ok bool) string {
	if !ok || s.Status == "running" || s.Duration <= 0 {
		return ""
	}
	return dimStyle.Render(compactDuration(s.Duration))
}

// check renders one doctor line: a green ✓ or red ✗, a label, and an optional
// detail (already styled by the caller).
func check(ok bool, label, detail string) string {
	glyph := okStyle.Render(glyphOK)
	if !ok {
		glyph = failStyle.Render(glyphFail)
	}
	line := glyph + " " + label
	if detail != "" {
		line += "   " + detail
	}
	return line
}

// applyLine summarizes an apply: a headline plus per-category counts. Non-zero
// counts get their semantic color; zero counts and unchanged stay dim.
func applyLine(s launchd.Summary) string {
	head := okStyle.Render(glyphOK + " applied")
	if len(s.Errors) > 0 {
		head = failStyle.Render(glyphFail + " applied with errors")
	}
	count := func(st lipgloss.Style, n int, sign, word string) string {
		text := fmt.Sprintf("%s%d %s", sign, n, word)
		if n == 0 {
			return dimStyle.Render(text)
		}
		return st.Render(text)
	}
	parts := []string{
		count(okStyle, s.Created, "+", "created"),
		count(warnStyle, s.Updated, "~", "updated"),
		count(failStyle, s.Removed, "-", "removed"),
		dimStyle.Render(fmt.Sprintf("%d unchanged", s.Unchanged)),
	}
	return head + "   " + strings.Join(parts, "  ")
}

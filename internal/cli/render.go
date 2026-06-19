package cli

import (
	"fmt"
	"strings"

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

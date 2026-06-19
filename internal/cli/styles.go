package cli

import "github.com/charmbracelet/lipgloss"

// Semantic palette shared by the render helpers. Colors are ANSI indices so
// lipgloss's default renderer can downsample them to a terminal's profile and
// strip them entirely for non-TTY / NO_COLOR output.
var (
	okStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true) // green: success
	errStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)  // red: error prefixes
	failStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))             // red: failed state
	warnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))            // yellow: stale/paused
	runStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))            // cyan: running

	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8")) // gray: secondary detail
	labelStyle  = dimStyle                                            // kv keys
	infoStyle   = dimStyle                                            // standalone info lines
	headerStyle = lipgloss.NewStyle().Bold(true)                      // column headers
)

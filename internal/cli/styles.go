package cli

import "github.com/charmbracelet/lipgloss"

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	okStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	infoStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

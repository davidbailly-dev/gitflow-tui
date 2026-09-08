package tui

import (
	"github.com/charmbracelet/lipgloss"

	"gitflow-tui/internal/gitflow"
)

var (
	colorMain    = lipgloss.Color("204")
	colorDevelop = lipgloss.Color("39")
	colorFeature = lipgloss.Color("42")
	colorRelease = lipgloss.Color("214")
	colorHotfix  = lipgloss.Color("196")
	colorOther   = lipgloss.Color("245")
	colorWarning = lipgloss.Color("208")

	styleHeader        = lipgloss.NewStyle().Bold(true).Padding(0, 1).Background(lipgloss.Color("236")).Foreground(lipgloss.Color("255"))
	styleHeaderLive    = lipgloss.NewStyle().Bold(true).Padding(0, 1).Background(lipgloss.Color("28")).Foreground(lipgloss.Color("255"))
	styleFooter        = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Padding(0, 1)
	styleColumnTitle   = lipgloss.NewStyle().Bold(true).Underline(true)
	styleSelected      = lipgloss.NewStyle().Reverse(true)
	styleColumn        = lipgloss.NewStyle().Padding(0, 1).Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("240"))
	styleColumnFocused = lipgloss.NewStyle().Padding(0, 1).Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("255"))
	styleFaint         = lipgloss.NewStyle().Faint(true)
)

func colorFor(t gitflow.BranchType) lipgloss.Color {
	switch t {
	case gitflow.TypeMain:
		return colorMain
	case gitflow.TypeDevelop:
		return colorDevelop
	case gitflow.TypeFeature:
		return colorFeature
	case gitflow.TypeRelease:
		return colorRelease
	case gitflow.TypeHotfix:
		return colorHotfix
	default:
		return colorOther
	}
}

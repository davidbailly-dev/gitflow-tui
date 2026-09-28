package tui

import (
	"github.com/charmbracelet/lipgloss"

	"gitflow-tui/internal/gitflow"
)

// Palette. Chaque couleur n'a qu'un sens : les types de branches ont chacun
// la leur, l'orange est réservé aux écarts GitFlow (alertes), et les commits
// de fusion ont une couleur neutre distincte — une fusion n'est pas une
// anomalie.
var (
	colorMain    = lipgloss.Color("170") // orchidée
	colorDevelop = lipgloss.Color("39")  // bleu
	colorFeature = lipgloss.Color("42")  // vert
	colorBugfix  = lipgloss.Color("79")  // vert d'eau
	colorRelease = lipgloss.Color("220") // jaune
	colorHotfix  = lipgloss.Color("196") // rouge
	colorSupport = lipgloss.Color("141") // lavande
	colorOther   = lipgloss.Color("245") // gris
	colorWarning = lipgloss.Color("208") // orange : alertes uniquement
	colorMerge   = lipgloss.Color("110") // bleu-gris : commits de fusion

	colorDiffAdd  = lipgloss.Color("71")
	colorDiffDel  = lipgloss.Color("167")
	colorDiffHunk = lipgloss.Color("73")
	colorDiffHash = lipgloss.Color("179")

	styleHeader        = lipgloss.NewStyle().Bold(true).Padding(0, 1).Background(lipgloss.Color("236")).Foreground(lipgloss.Color("255"))
	styleHeaderLive    = lipgloss.NewStyle().Bold(true).Padding(0, 1).Background(lipgloss.Color("28")).Foreground(lipgloss.Color("255"))
	styleFooter        = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Padding(0, 1)
	styleColumnTitle   = lipgloss.NewStyle().Bold(true).Underline(true)
	styleColumn        = lipgloss.NewStyle().Padding(0, 1).Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("240"))
	styleColumnFocused = lipgloss.NewStyle().Padding(0, 1).Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("255"))
	styleFaint         = lipgloss.NewStyle().Faint(true)
)

// selectionStyle applique à base le surlignage de la ligne sélectionnée :
// franc (inversion vidéo) dans le panneau actif, discret (fond gris) dans
// un panneau inactif, pour voir d'un coup d'œil où va la navigation tout en
// gardant la sélection des autres panneaux visible.
func selectionStyle(base lipgloss.Style, focused bool) lipgloss.Style {
	if focused {
		return base.Reverse(true)
	}
	return base.Background(lipgloss.Color("238"))
}

func colorFor(t gitflow.BranchType) lipgloss.Color {
	switch t {
	case gitflow.TypeMain:
		return colorMain
	case gitflow.TypeDevelop:
		return colorDevelop
	case gitflow.TypeFeature:
		return colorFeature
	case gitflow.TypeBugfix:
		return colorBugfix
	case gitflow.TypeRelease:
		return colorRelease
	case gitflow.TypeHotfix:
		return colorHotfix
	case gitflow.TypeSupport:
		return colorSupport
	default:
		return colorOther
	}
}

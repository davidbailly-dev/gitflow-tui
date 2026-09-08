package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"gitflow-tui/internal/git"
	"gitflow-tui/internal/gitflow"
)

func (m Model) View() string {
	if m.err != nil {
		return fmt.Sprintf("gitflow-tui: erreur : %v\n", m.err)
	}
	if m.loading {
		return "Chargement du dépôt...\n"
	}

	header := m.renderHeader()
	footer := m.renderFooter()

	if m.showHelp {
		return lipgloss.JoinVertical(lipgloss.Left, header, m.renderHelp(), footer)
	}

	var body string
	if m.view == viewGraph {
		body = m.graph.View()
	} else {
		body = m.renderColumns()
	}

	content := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	return m.padToHeight(content)
}

// padToHeight complète content avec des lignes vides jusqu'à occuper toute
// la hauteur du terminal. Sans ça, une frame plus courte que la précédente
// (ex. bascule vers une colonne vide, au détail plus court) laisse des
// lignes de l'ancienne frame non réécrites en bas de l'écran.
func (m Model) padToHeight(content string) string {
	if m.height <= 0 {
		return content
	}
	lines := strings.Count(content, "\n") + 1
	if lines >= m.height {
		return content
	}
	// Chaque ligne de complément doit couvrir toute la largeur du terminal :
	// un simple espace ne réécrit qu'une colonne, laissant subsister le
	// reste d'une ancienne ligne plus longue (ex. un pied de page complété
	// à la largeur du terminal) que le rendu ne réeffacerait pas forcément
	// avant d'écrire un contenu plus court.
	blank := strings.Repeat(" ", maxInt(m.width, 1))
	pad := make([]string, m.height-lines)
	for i := range pad {
		pad[i] = blank
	}
	return content + "\n" + strings.Join(pad, "\n")
}

func (m Model) renderHeader() string {
	viewName := "Colonnes"
	style := styleHeader
	if m.view == viewGraph {
		viewName = "Graphe — historique"
		if m.graphMode == graphLive {
			viewName = "Graphe — direct"
			style = styleHeaderLive
		}
	}
	title := fmt.Sprintf("gitflow-tui — vue: %s", viewName)
	return style.Width(maxInt(m.width, 0)).Render(title)
}

func (m Model) renderFooter() string {
	if m.filtering {
		return styleFooter.Width(maxInt(m.width, 0)).Render("Filtrer : " + m.filter + "█  (entrée: valider, échap: annuler)")
	}
	help := "tab: vue  ↑↓←→/hjkl: naviguer  /: filtrer  r: rafraîchir  ?: aide  q: quitter"
	if m.view == viewGraph {
		help = "tab: vue  m: historique/direct  ↑↓: défiler  r: rafraîchir  ?: aide  q: quitter"
	}
	if m.filter != "" {
		help = "filtre actif: " + m.filter + "  |  " + help
	}
	return styleFooter.Width(maxInt(m.width, 0)).Render(help)
}

func (m Model) renderHelp() string {
	lines := []string{
		"Aide — gitflow-tui",
		"",
		"tab            changer de vue (colonnes / graphe)",
		"m              basculer historique / direct (vue graphe)",
		"←/→, h/l       changer de panneau (branches / commits / contenu)",
		"↑/↓, j/k       se déplacer dans le panneau actif (ou défiler le contenu)",
		"/              filtrer les branches par nom (vue colonnes)",
		"r              rafraîchir les données",
		"?              afficher/masquer cette aide",
		"q, ctrl+c      quitter",
		"",
		"Appuie sur ? pour revenir.",
	}
	return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
}

// renderColumns dessine la vue colonnes : trois panneaux façon "colonnes
// Miller" — les branches groupées par type GitFlow, les commits/fusions de
// la branche sélectionnée, et le contenu complet (git show) du commit
// sélectionné. Le panneau qui a le focus (↑/↓ ou défilement) est bordé en
// clair.
func (m Model) renderColumns() string {
	if len(m.columns) == 0 {
		return "Aucune branche trouvée."
	}

	w1, w2, w3 := m.paneWidths()

	p1 := m.renderBranchPane(w1)
	p2 := m.renderCommitPane(w2)
	p3 := m.renderDiffPane(w3)

	return lipgloss.JoinHorizontal(lipgloss.Top, p1, p2, p3)
}

func paneBox(focused bool) lipgloss.Style {
	if focused {
		return styleColumnFocused
	}
	return styleColumn
}

// windowLines renvoie au plus height lignes de lines, en faisant défiler la
// fenêtre pour garder l'index selected visible, et en complétant par des
// lignes vides si lines tient déjà dans height.
func windowLines(lines []string, selected, height int) []string {
	if height <= 0 {
		return nil
	}
	if len(lines) <= height {
		out := make([]string, height)
		copy(out, lines)
		return out
	}
	start := selected - height/2
	if start < 0 {
		start = 0
	}
	if start > len(lines)-height {
		start = len(lines) - height
	}
	return lines[start : start+height]
}

// truncateLines coupe chaque ligne à width (en tenant compte des codes ANSI
// déjà appliqués) plutôt que de laisser lipgloss les retourner à la ligne :
// sur un terminal étroit, un retour à la ligne ferait dévier le nombre de
// lignes réellement affichées de celui attendu par windowLines, désalignant
// la hauteur du panneau par rapport aux deux autres.
func truncateLines(lines []string, width int) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = ansi.Truncate(l, width, "…")
	}
	return out
}

func (m Model) renderBranchPane(width int) string {
	contentH := m.paneContentHeight()
	rows := m.flatBranches()

	lines := make([]string, 0, len(rows))
	for i, row := range rows {
		switch {
		case row.header != "":
			lines = append(lines, styleColumnTitle.Render(row.header))
		case row.empty:
			lines = append(lines, styleFaint.Render("  (vide)"))
		default:
			line := renderItemLine(row.it)
			if i == m.branchIdx {
				line = styleSelected.Render(line)
			}
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		lines = []string{styleFaint.Render("Aucune branche trouvée.")}
	}
	lines = truncateLines(lines, maxInt(width-2, 1))

	body := windowLines(lines, m.branchIdx, contentH)
	box := paneBox(m.paneFocus == paneBranches)
	return box.Width(width).Height(contentH).Render(strings.Join(body, "\n"))
}

func (m Model) renderCommitPane(width int) string {
	contentH := m.paneContentHeight()

	header := styleColumnTitle.Render("Commits / fusions")
	var lines []string
	it, ok := m.selectedBranchRow()
	switch {
	case !ok:
		lines = []string{styleFaint.Render("Aucune branche sélectionnée.")}
	case len(it.commits) == 0:
		header = styleColumnTitle.Render(it.node.Name)
		lines = []string{styleFaint.Render("(aucun commit)")}
	default:
		header = styleColumnTitle.Render(it.node.Name)
		for i, c := range it.commits {
			lines = append(lines, renderCommitLine(c, i == m.commitIdx))
		}
	}

	innerWidth := maxInt(width-2, 1)
	header = ansi.Truncate(header, innerWidth, "…")
	lines = truncateLines(lines, innerWidth)

	body := windowLines(lines, m.commitIdx, maxInt(contentH-1, 1))
	box := paneBox(m.paneFocus == paneCommits)
	content := header + "\n" + strings.Join(body, "\n")
	return box.Width(width).Height(contentH).Render(content)
}

func renderCommitLine(c git.Commit, selected bool) string {
	marker := " "
	color := colorFeature
	if c.IsMerge {
		marker = "⑂"
		color = colorWarning
	}
	style := lipgloss.NewStyle().Foreground(color)
	if selected {
		style = style.Reverse(true)
	}
	return style.Render(fmt.Sprintf("%s %s %s — %s", marker, c.Hash, shortDate(c.Date), c.Subject))
}

// mentionedBranch cherche une mention de branche feature/release/hotfix
// dans subject (message d'un commit de fusion) et renvoie son nom classifié
// GitFlow, pour afficher la branche source d'une fusion plutôt que sa seule
// cible.
func mentionedBranch(subject string) (name string, branchType gitflow.BranchType, ok bool) {
	name = branchMentionRe.FindString(subject)
	if name == "" {
		return "", 0, false
	}
	return name, gitflow.Classify([]string{name})[0].Type, true
}

// renderDiffPane dessine le panneau "contenu". Son en-tête rappelle la
// branche concernée, colorée selon son type GitFlow : la branche source
// d'une fusion (ex. feature/x, mentionnée dans le message) si elle est
// identifiable, sinon celle sélectionnée au panneau 1 — sans quoi on la
// perd de vue dès que le focus en sort.
func (m Model) renderDiffPane(width int) string {
	innerWidth := maxInt(width-2, 1)

	c, hasCommit := m.selectedCommit()

	var branchLabel string
	if hasCommit && c.IsMerge {
		if name, branchType, found := mentionedBranch(c.Subject); found {
			branchLabel = lipgloss.NewStyle().Bold(true).Foreground(colorFor(branchType)).Render(name)
		}
	}
	if branchLabel == "" {
		if it, ok := m.selectedBranchRow(); ok {
			branchLabel = lipgloss.NewStyle().Bold(true).Foreground(colorFor(it.node.Type)).Render(it.node.Name)
		}
	}

	title := "Contenu"
	if hasCommit {
		title = fmt.Sprintf("%s — %s", c.Hash, c.Subject)
	}

	sep := ""
	if branchLabel != "" {
		sep = " › "
	}
	titleWidth := maxInt(innerWidth-lipgloss.Width(branchLabel)-lipgloss.Width(sep), 1)
	title = styleColumnTitle.Render(ansi.Truncate(title, titleWidth, "…"))

	header := ansi.Truncate(branchLabel+sep+title, innerWidth, "…")
	box := paneBox(m.paneFocus == paneDiff)
	content := header + "\n" + m.diff.View()
	return box.Width(width).Height(m.paneContentHeight()).Render(content)
}

func renderItemLine(it item) string {
	marker := " "
	if it.branch.IsHead {
		marker = "●"
	}
	status := ""
	if it.ahead > 0 || it.behind > 0 {
		status = fmt.Sprintf(" ↑%d↓%d", it.ahead, it.behind)
	}

	style := lipgloss.NewStyle().Foreground(colorFor(it.node.Type))
	if it.branch.IsHead {
		style = style.Bold(true)
	}
	return style.Render(fmt.Sprintf("%s %s%s", marker, it.node.Name, status))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

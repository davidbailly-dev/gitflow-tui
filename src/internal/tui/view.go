package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
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
		return m.padToHeight(lipgloss.JoinVertical(lipgloss.Left, header, m.renderHelp(), footer))
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

// renderHeader affiche à gauche le dépôt, la branche courante et la vue
// active, à droite l'état des données : rechargement en cours, ou heure du
// dernier chargement.
func (m Model) renderHeader() string {
	viewName := "Colonnes"
	style := styleHeader
	if m.view == viewGraph {
		switch {
		case m.graphMode == graphLive:
			viewName = "Graphe — direct"
			style = styleHeaderLive
		case m.historyAlertsOnly:
			viewName = "Graphe — historique (écarts)"
		default:
			viewName = "Graphe — historique (complet)"
		}
	}

	branch := "HEAD détachée"
	if m.currentBranch != "" {
		branch = "● " + m.currentBranch
	}
	left := fmt.Sprintf("gitflow-tui · %s · %s · vue: %s", filepath.Base(m.repo.Root()), branch, viewName)

	right := ""
	switch {
	case m.refreshing:
		right = "⟳ rafraîchissement…"
	case !m.loadedAt.IsZero():
		right = "à jour " + m.loadedAt.Format("15:04:05")
	}

	// La largeur du style comprend son padding horizontal (1 de chaque côté).
	inner := maxInt(m.width-2, 0)
	if lipgloss.Width(left)+1+lipgloss.Width(right) > inner {
		right = ""
	}
	left = ansi.Truncate(left, inner, "…")
	gap := maxInt(inner-lipgloss.Width(left)-lipgloss.Width(right), 0)
	return style.Width(maxInt(m.width, 0)).Render(left + strings.Repeat(" ", gap) + right)
}

// renderFooter affiche les raccourcis utiles dans la vue active, ou la
// saisie du filtre. Le texte est tronqué à la largeur du terminal : replié
// sur deux lignes, il repousserait l'en-tête hors de l'écran.
func (m Model) renderFooter() string {
	inner := maxInt(m.width-2, 0) // padding horizontal du style
	if m.filtering {
		return styleFooter.Width(maxInt(m.width, 0)).Render(ansi.Truncate("Filtrer : "+m.filter+"█  (entrée: valider, échap: annuler)", inner, "…"))
	}
	bindings := []key.Binding{keys.Tab, keys.Navigate, keys.Filter}
	if m.view == viewGraph {
		bindings = []key.Binding{keys.Tab, keys.Mode}
		if m.graphMode == graphHistory {
			bindings = append(bindings, keys.Alerts)
		}
		bindings = append(bindings, keys.Scroll)
	}
	bindings = append(bindings, keys.Refresh, keys.Help, keys.Quit)

	help := shortHelp(bindings...)
	if m.filter != "" {
		help = "filtre actif: " + m.filter + "  |  " + help
	}
	return styleFooter.Width(maxInt(m.width, 0)).Render(ansi.Truncate(help, inner, "…"))
}

// shortHelp résume des raccourcis sur une ligne ("tab: changer de vue  …").
func shortHelp(bindings ...key.Binding) string {
	parts := make([]string, 0, len(bindings))
	for _, b := range bindings {
		h := b.Help()
		parts = append(parts, h.Key+": "+h.Desc)
	}
	return strings.Join(parts, "  ")
}

// helpSection liste des raccourcis sous un titre, un par ligne.
func helpSection(title string, bindings ...key.Binding) []string {
	lines := []string{styleColumnTitle.Render(title)}
	for _, b := range bindings {
		h := b.Help()
		lines = append(lines, fmt.Sprintf("  %-12s %s", h.Key, h.Desc))
	}
	return append(lines, "")
}

// legendSection explique un symbole par ligne, sous un titre.
func legendSection(title string, entries ...[2]string) []string {
	lines := []string{styleColumnTitle.Render(title)}
	for _, e := range entries {
		lines = append(lines, fmt.Sprintf("  %-12s %s", e[0], e[1]))
	}
	return append(lines, "")
}

// renderHelp affiche les raccourcis à gauche et la légende des symboles à
// droite, côte à côte pour tenir dans un terminal de hauteur modeste.
func (m Model) renderHelp() string {
	var shortcuts []string
	shortcuts = append(shortcuts, helpSection("Général", keys.Tab, keys.Refresh, keys.Help, keys.Quit)...)
	shortcuts = append(shortcuts, helpSection("Vue colonnes", keys.Up, keys.Down, keys.Left, keys.Right, keys.Filter)...)
	shortcuts = append(shortcuts, helpSection("Vue graphe", keys.Mode, keys.Alerts, keys.Scroll)...)

	var symbols []string
	symbols = append(symbols, legendSection("Branches",
		[2]string{"●", "branche courante"},
		[2]string{"↑n", "commits pas encore dans la branche parente"},
		[2]string{"↓n", "commits de la parente absents de la branche"},
		[2]string{"✔", "fusionnée dans toutes ses cibles"},
		[2]string{"⚠", "fusionnée dans une partie de ses cibles"},
		[2]string{"⑂", "commit de fusion"},
	)...)
	symbols = append(symbols, legendSection("Graphe",
		[2]string{"┬ │ └─▶", "départ ou fusion d'une branche"},
		[2]string{"○", "en cours"},
		[2]string{"×", "supprimée (reconstituée)"},
		[2]string{"⚠", "écart au workflow GitFlow"},
	)...)

	columns := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().PaddingRight(6).Render(strings.Join(shortcuts, "\n")),
		strings.Join(symbols, "\n"),
	)
	lines := []string{
		"Aide — gitflow-tui",
		"",
		columns,
		styleFaint.Render("Le dépôt est surveillé : l'affichage se met à jour de lui-même après un"),
		styleFaint.Render("commit, une fusion, un changement ou une suppression de branche."),
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

	focused := m.paneFocus == paneBranches
	lines := make([]string, 0, len(rows))
	for i, row := range rows {
		if row.header != "" {
			lines = append(lines, styleColumnTitle.Render(row.header)+styleFaint.Render(fmt.Sprintf(" %d", row.count)))
			continue
		}
		lines = append(lines, renderItemLine(row.it, i == m.branchIdx, focused))
	}
	if len(lines) == 0 {
		msg := "Aucune branche trouvée."
		if m.filter != "" {
			msg = "Aucune branche ne correspond au filtre."
		}
		lines = []string{styleFaint.Render(msg)}
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
			lines = append(lines, renderCommitLine(c, i == m.commitIdx, m.paneFocus == paneCommits))
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

// renderCommitLine dessine un commit de la liste : hash et date atténués,
// sujet en clair, ou en couleur de fusion (avec "⑂") pour un commit de
// fusion. La ligne sélectionnée est surlignée d'un seul tenant.
func renderCommitLine(c git.Commit, selected, focused bool) string {
	marker := " "
	subject := lipgloss.NewStyle()
	if c.IsMerge {
		marker = "⑂"
		subject = subject.Foreground(colorMerge)
	}
	if selected {
		return selectionStyle(subject, focused).Render(fmt.Sprintf("%s %s %s — %s", marker, c.Hash, shortDate(c.Date), c.Subject))
	}
	meta := styleFaint.Render(fmt.Sprintf("%s %s —", c.Hash, shortDate(c.Date)))
	return subject.Render(marker) + " " + meta + " " + subject.Render(c.Subject)
}

// mergedBranch renvoie la branche source d'un commit de fusion, lue dans
// son message, et son type GitFlow, pour afficher la branche intégrée par
// une fusion plutôt que sa seule cible.
func mergedBranch(subject string) (name string, branchType gitflow.BranchType, ok bool) {
	merge, ok := gitflow.ParseMerge(subject)
	if !ok {
		return "", 0, false
	}
	return merge.Source, gitflow.Classify([]string{merge.Source})[0].Type, true
}

// renderDiffPane dessine le panneau "contenu". Son en-tête rappelle la
// branche concernée, colorée selon son type GitFlow : la branche source
// d'une fusion (ex. feature/x, lue dans le message) si elle est
// identifiable, sinon celle sélectionnée au panneau 1 — sans quoi on la
// perd de vue dès que le focus en sort.
func (m Model) renderDiffPane(width int) string {
	innerWidth := maxInt(width-2, 1)

	c, hasCommit := m.selectedCommit()

	var branchLabel string
	if hasCommit && c.IsMerge {
		if name, branchType, found := mergedBranch(c.Subject); found {
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

// renderItemLine dessine une branche du panneau de gauche, dans la couleur
// de son type : "✔" si elle est fusionnée dans toutes ses cibles, "⚠" si
// seulement dans certaines, sinon son avance (↑) et son retard (↓) sur sa
// branche parente, zéros omis.
func renderItemLine(it item, selected, focused bool) string {
	marker := " "
	if it.branch.IsHead {
		marker = "●"
	}
	style := lipgloss.NewStyle().Foreground(colorFor(it.node.Type))
	if it.branch.IsHead {
		style = style.Bold(true)
	}

	var status string
	statusStyle := styleFaint
	switch it.status {
	case statusMerged:
		status = "✔"
	case statusPartial:
		status = "⚠"
		statusStyle = lipgloss.NewStyle().Foreground(colorWarning)
	default:
		var parts []string
		if it.ahead > 0 {
			parts = append(parts, fmt.Sprintf("↑%d", it.ahead))
		}
		if it.behind > 0 {
			parts = append(parts, fmt.Sprintf("↓%d", it.behind))
		}
		status = strings.Join(parts, " ")
	}

	text := marker + " " + it.node.Name
	if selected {
		if status != "" {
			text += " " + status
		}
		return selectionStyle(style, focused).Render(text)
	}
	if status == "" {
		return style.Render(text)
	}
	return style.Render(text) + " " + statusStyle.Render(status)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

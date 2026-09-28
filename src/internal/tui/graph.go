package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"gitflow-tui/internal/gitflow"
)

// cellWidth est la largeur en caractères d'une "colonne" chronologique dans
// le diagramme en couloirs horizontaux.
const cellWidth = 3

// laneLabelWidth est la largeur du préfixe (marqueur + nom de ligne) placé
// avant le corps du diagramme sur les lignes main/develop : la colonne col
// du diagramme se trouve donc à laneLabelWidth + col*cellWidth caractères
// du bord (cf. laneX), sur la ligne permanente comme sur les lignes des
// branches, dont le repère "└─▶" tombe ainsi sous le "┬" correspondant.
const laneLabelWidth = 11

// maxCommitsShown limite le nombre de commits listés sous chaque fusion.
const maxCommitsShown = 4

// graphLane est une ligne de l'historique GitFlow placée dans le
// diagramme : col est sa colonne sur l'axe du temps.
type graphLane struct {
	gitflow.Lane
	col int
}

// graphData est ce que montre la vue graphe : les deux lignes permanentes,
// les commits directs à signaler, la prochaine release et les lignes de
// branches retenues par le mode d'affichage.
type graphData struct {
	main, develop             *gitflow.Spine
	mainDirect, developDirect []gitflow.Commit
	unreleased                int
	lanes                     []graphLane
}

// graphMode sélectionne ce que montre la vue graphe.
type graphMode int

const (
	// graphHistory : l'historique complet (branches supprimées
	// reconstituées, synchronisations, écarts), éventuellement réduit aux
	// seuls écarts.
	graphHistory graphMode = iota
	// graphLive : l'état courant — les branches présentes pas encore
	// complètement fusionnées, sans les écarts qui relèvent de l'historique
	// (commits directs, fusions hors flow).
	graphLive
)

// newGraphData sélectionne, dans le rapport, ce que montre la vue graphe
// selon le mode : tout l'historique, ses seuls écarts (deviationsOnly), ou
// le travail en cours (graphLive).
func newGraphData(r gitflow.Report, mode graphMode, deviationsOnly bool) graphData {
	g := graphData{main: r.Main, develop: r.Develop, unreleased: r.Unreleased}
	if mode == graphHistory {
		if r.Main != nil {
			g.mainDirect = r.Main.Direct
		}
		if r.Develop != nil {
			g.developDirect = r.Develop.Direct
		}
	}
	for _, l := range r.Lanes {
		switch {
		case mode == graphLive:
			if !l.IsPending() {
				continue
			}
			l.Unexpected = nil
		case deviationsOnly && len(l.Deviations()) == 0:
			continue
		}
		g.lanes = append(g.lanes, graphLane{Lane: l, col: len(g.lanes)})
	}
	return g
}

// laneX renvoie la position horizontale (en caractères) du repère d'une
// ligne, identique sur la ligne permanente ("┬") et sur la ligne de la
// branche ("└─▶").
func laneX(l graphLane) int {
	return laneLabelWidth + l.col*cellWidth
}

// rails dessine les width premiers caractères d'une ligne du diagramme : un
// trait vertical "│" sous le repère de chaque ligne encore à venir (open),
// qui relie ce repère sur la ligne permanente à la ligne de sa branche, et
// des espaces ailleurs.
func rails(width int, open []graphLane) string {
	at := make(map[int]graphLane, len(open))
	for _, l := range open {
		at[laneX(l)] = l
	}
	var b strings.Builder
	for x := 0; x < width; x++ {
		if l, ok := at[x]; ok {
			b.WriteString(lipgloss.NewStyle().Foreground(laneColor(l)).Faint(l.Kind == gitflow.LaneDeleted).Render("│"))
		} else {
			b.WriteByte(' ')
		}
	}
	return b.String()
}

// renderGraph dessine le diagramme : chaque ligne permanente, suivie des
// lignes de branches qui s'y rattachent, de la plus récente (repère le plus
// à droite) à la plus ancienne. Dans cet ordre, les traits verticaux des
// lignes restant à dessiner passent toujours à gauche du texte de la ligne
// courante, sans jamais le croiser.
//
// Il renvoie aussi, dans l'ordre d'affichage, la position (numéro de ligne)
// de chaque ligne de branche, pour la sélection au clavier ; selectedKey
// désigne la ligne sélectionnée (cf. laneKey), dont le nom est surligné.
func renderGraph(g graphData, emptyMsg, selectedKey string) (string, []graphAnchor) {
	if g.main == nil && g.develop == nil {
		return "Aucune branche main/develop trouvée.", nil
	}

	spineLen := (len(g.lanes)+1)*cellWidth + 2
	if spineLen < 16 {
		spineLen = 16
	}

	var mainLanes, developLanes []graphLane
	for i := len(g.lanes) - 1; i >= 0; i-- {
		if g.lanes[i].TargetsMain() {
			mainLanes = append(mainLanes, g.lanes[i])
		} else {
			developLanes = append(developLanes, g.lanes[i])
		}
	}

	var b strings.Builder
	var anchors []graphAnchor
	if axis := renderAxis(g.lanes); axis != "" {
		b.WriteString(axis)
		b.WriteString("\n")
	}

	writeGroup := func(color lipgloss.Color, spine *gitflow.Spine, direct []gitflow.Commit, group []graphLane) {
		if spine == nil {
			return
		}
		b.WriteString(renderSpine(color, *spine, spineLen, g.lanes, group))
		b.WriteString("\n")
		if len(group) > 0 {
			b.WriteString(rails(laneX(group[0])+1, group))
			b.WriteString("\n")
		}
		for i, l := range group {
			anchors = append(anchors, graphAnchor{line: strings.Count(b.String(), "\n"), lane: l})
			b.WriteString(renderLane(l, group[i+1:], laneKey(l) == selectedKey))
			b.WriteString("\n")
		}
		if spine == g.develop && g.unreleased > 0 && g.main != nil {
			b.WriteString(strings.Repeat(" ", laneLabelWidth))
			b.WriteString(styleFaint.Render(fmt.Sprintf("↑ %d commit(s) pas encore dans %s — contenu de la prochaine release", g.unreleased, g.main.Branch.Name)))
			b.WriteString("\n")
		}
		if block := renderDirectCommits(spine.Branch.Name, direct); block != "" {
			b.WriteString(block)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	writeGroup(colorMain, g.main, g.mainDirect, mainLanes)
	writeGroup(colorDevelop, g.develop, g.developDirect, developLanes)

	if len(g.lanes) == 0 && len(g.mainDirect) == 0 && len(g.developDirect) == 0 {
		b.WriteString(styleFaint.Render(emptyMsg))
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n"), anchors
}

// graphAnchor situe une ligne de branche dans le rendu du graphe.
type graphAnchor struct {
	line int
	lane graphLane
}

// laneKey identifie une ligne du graphe d'un rendu à l'autre, pour garder
// la sélection après un rechargement ou un changement de mode. Le nom d'une
// branche suffit (il est unique parmi les lignes) et reste valable quand
// elle change d'état sous les yeux de l'utilisateur : fusionnée, sa date
// change ; supprimée, elle passe de présente à reconstituée. Les
// synchronisations develop → main, qui portent toutes le même nom, se
// distinguent par leur date, qui ne change jamais.
func laneKey(l graphLane) string {
	if l.Kind == gitflow.LaneSync {
		return "sync|" + l.Date
	}
	return "branche|" + l.Node.Name
}

// renderAxis dessine l'axe du temps, au-dessus des lignes permanentes :
// l'année dans la marge (celle de la première date), puis la date (MM-JJ)
// de chaque jour sous la première colonne de ce jour, alignée sur son
// repère "┬" — ou la date complète quand l'année change. Une date qui
// chevaucherait la précédente est omise plutôt que décalée sous une autre
// colonne, où elle serait trompeuse. lanes doit être trié par colonne.
func renderAxis(lanes []graphLane) string {
	var b strings.Builder
	end := 0 // première position libre après le dernier texte écrit
	prevDay, year := "", ""
	for _, l := range lanes {
		if len(l.Date) < 10 || l.Date[:10] == prevDay {
			continue
		}
		day := l.Date[:10]
		prevDay = day
		if year == "" {
			year = day[:4]
			gutter := fmt.Sprintf("  %-*s", laneLabelWidth-2, year)
			b.WriteString(gutter)
			end = len(gutter)
		}
		label := day[5:]
		if day[:4] != year {
			label = day
		}
		x := laneX(l)
		if x < end || (x == end && end > laneLabelWidth) {
			continue
		}
		b.WriteString(strings.Repeat(" ", x-end))
		b.WriteString(label)
		end = x + len(label)
		year = day[:4]
	}
	if b.Len() == 0 {
		return ""
	}
	return styleFaint.Render(b.String())
}

// renderDirectCommits affiche, à la suite des lignes de branches d'une ligne
// permanente (dont les traits verticaux occupent l'espace juste en
// dessous), les commits qui y ont été committés directement plutôt que d'y
// arriver par une fusion — un contournement complet du workflow GitFlow.
func renderDirectCommits(spineLabel string, commits []gitflow.Commit) string {
	if len(commits) == 0 {
		return ""
	}
	indent := strings.Repeat(" ", laneLabelWidth)
	warnStyle := lipgloss.NewStyle().Foreground(colorWarning)
	commitStyle := lipgloss.NewStyle().Faint(true)

	lines := []string{indent + warnStyle.Render(fmt.Sprintf(
		"⚠ %d commit(s) directement sur %s (hors fusion, hors GitFlow)", len(commits), spineLabel))}

	shown := commits
	more := 0
	if len(shown) > maxCommitsShown {
		more = len(shown) - maxCommitsShown
		shown = shown[:maxCommitsShown]
	}
	for _, c := range shown {
		lines = append(lines, commitStyle.Render(fmt.Sprintf("%s    · %s %s", indent, c.Short(), c.Subject)))
	}
	if more > 0 {
		lines = append(lines, commitStyle.Render(fmt.Sprintf("%s    … +%d commit(s)", indent, more)))
	}
	return strings.Join(lines, "\n")
}

// renderSpine dessine la ligne permanente (main ou develop) : un trait
// continu marqué d'un repère "┬" à chaque colonne où une ligne part d'ici,
// y a été fusionnée, ou y a été fusionnée en dehors du flow attendu (repère
// alors en couleur d'alerte) — de quoi voir en un coup d'œil tout ce qui a
// atterri dans main, conforme ou non. Les lignes de group, dessinées sous
// cette ligne permanente, y ont toujours un repère, point de départ de leur
// trait vertical. Les repères des branches supprimées sont atténués.
func renderSpine(spineColor lipgloss.Color, spine gitflow.Spine, length int, lanes, group []graphLane) string {
	inGroup := make(map[int]bool, len(group))
	for _, l := range group {
		inGroup[l.col] = true
	}

	cells := make([]rune, length)
	for i := range cells {
		cells[i] = '─'
	}
	marks := make(map[int]lipgloss.Color, len(lanes))
	faint := make(map[int]bool, len(lanes))
	for _, l := range lanes {
		concerned := inGroup[l.col] || l.Node.Parent == spine.Branch.Name
		anomaly := false
		if !concerned {
			for _, mr := range l.Merged {
				if mr.Target == spine.Branch.Name {
					concerned = true
					break
				}
			}
		}
		if !concerned {
			for _, mr := range l.Unexpected {
				if mr.Target == spine.Branch.Name {
					concerned = true
					anomaly = true
					break
				}
			}
		}
		if !concerned {
			continue
		}
		pos := l.col * cellWidth
		if pos >= 0 && pos < length {
			cells[pos] = '┬'
			if anomaly {
				marks[pos] = colorWarning
			} else {
				marks[pos] = laneColor(l)
			}
			faint[pos] = l.Kind == gitflow.LaneDeleted
		}
	}

	var body strings.Builder
	start := 0
	spineStyle := lipgloss.NewStyle().Foreground(spineColor)
	flush := func(end int) {
		if end > start {
			body.WriteString(spineStyle.Render(string(cells[start:end])))
		}
	}
	for i, c := range cells {
		if c == '┬' {
			flush(i)
			style := lipgloss.NewStyle().Foreground(marks[i]).Faint(faint[i])
			body.WriteString(style.Render("┬"))
			start = i + 1
		}
	}
	flush(length)

	marker := " "
	if spine.Branch.IsHead {
		marker = "●"
	}
	// Nom tronqué pour ne pas décaler l'axe du temps (cf. laneLabelWidth),
	// en gardant une espace avant le trait.
	name := ansi.Truncate(spine.Branch.Name, laneLabelWidth-3, "…")
	head := lipgloss.NewStyle().Foreground(spineColor).Bold(true).Render(fmt.Sprintf("%s %-*s", marker, laneLabelWidth-2, name))
	return head + body.String()
}

// renderLane dessine la ligne d'une branche ou d'une synchronisation : un
// repère aligné sous celui de sa ligne permanente (les traits verticaux des
// lignes encore à dessiner, open, passant à sa gauche), son nom, ses cibles de
// fusion, son statut, une éventuelle fusion hors GitFlow, et la liste
// (tronquée) des commits apportés. Les branches supprimées sont affichées
// en atténué avec la mention "supprimée" ; toute fusion en dehors du flow
// GitFlow attendu (develop → main direct, feature → main, fusion
// incomplète...) est signalée en couleur d'alerte.
func renderLane(l graphLane, open []graphLane, selected bool) string {
	color := laneColor(l)
	indent := rails(laneX(l), open)
	subIndent := indent + "    "
	faint := l.Kind == gitflow.LaneDeleted
	warnStyle := lipgloss.NewStyle().Foreground(colorWarning)

	connStyle := lipgloss.NewStyle().Foreground(color).Faint(faint)
	connector := connStyle.Render("└─▶")

	nameStyle := lipgloss.NewStyle().Foreground(color).Faint(faint)
	if l.Kind == gitflow.LaneLive && l.Branch.IsHead {
		nameStyle = nameStyle.Bold(true)
	}

	marker := " "
	switch {
	case l.Kind == gitflow.LaneDeleted:
		marker = "×"
	case l.Kind == gitflow.LaneSync:
		marker = "⚠"
	case l.Branch.IsHead:
		marker = "●"
	}

	var parts []string
	for _, mr := range l.Merged {
		part := mr.Target
		if mr.Date != "" {
			part += fmt.Sprintf(" (%s)", shortDate(mr.Date))
		}
		if mr.Tag != "" {
			part += " " + lipgloss.NewStyle().Foreground(colorRelease).Render("◆ "+mr.Tag)
		}
		parts = append(parts, part)
	}
	// Une branche supprimée ne sera plus fusionnée : sa cible manquante
	// n'est pas "en attente" mais définitivement ratée.
	pendingLabel := " (en attente)"
	if l.Kind == gitflow.LaneDeleted {
		pendingLabel = " (jamais fusionnée)"
	}
	for _, t := range l.Pending {
		parts = append(parts, warnStyle.Render(t+pendingLabel))
	}
	targetTxt := "—"
	if len(parts) > 0 {
		targetTxt = strings.Join(parts, ", ")
	}

	var status string
	switch {
	case l.Kind == gitflow.LaneDeleted && len(l.Pending) == 0:
		status = "✔ fusionnée (supprimée)"
	case l.Kind == gitflow.LaneDeleted && len(l.Merged) > 0:
		status = warnStyle.Render("⚠ partiellement fusionnée (supprimée)")
	case l.Kind == gitflow.LaneDeleted:
		status = "× supprimée"
	case l.Kind == gitflow.LaneSync:
		status = warnStyle.Render("⚠ fusion directe (hors GitFlow)")
	case len(l.Pending) == 0 && len(l.Merged) > 0:
		status = "✔ fusionnée"
	case len(l.Merged) > 0:
		status = warnStyle.Render("⚠ partiellement fusionnée")
	default:
		status = "○ en cours"
	}

	lines := []string{fmt.Sprintf("%s%s %s%s  → %s   %s",
		indent, connector, marker, nameText(nameStyle, l.Node.Name, selected), targetTxt, status)}

	for _, d := range l.Deviations() {
		if txt := deviationText(d, l.Lane); txt != "" {
			lines = append(lines, subIndent+warnStyle.Render(txt))
		}
	}

	if len(l.Commits) > 0 {
		commitStyle := lipgloss.NewStyle().Faint(true)
		shown := l.Commits
		more := 0
		if len(shown) > maxCommitsShown {
			more = len(shown) - maxCommitsShown
			shown = shown[:maxCommitsShown]
		}
		for _, c := range shown {
			lines = append(lines, commitStyle.Render(fmt.Sprintf("%s· %s %s", subIndent, c.Short(), c.Subject)))
		}
		if more > 0 {
			lines = append(lines, commitStyle.Render(fmt.Sprintf("%s… +%d commit(s)", subIndent, more)))
		}
	}

	return strings.Join(lines, "\n")
}

// deviationText décrit un écart sur sa propre ligne, sous celle de la
// branche. Les écarts déjà exprimés par le statut de la branche (fusion
// partielle, synchronisation directe) n'en ont pas.
func deviationText(d gitflow.Deviation, l gitflow.Lane) string {
	switch d.Kind {
	case gitflow.DeviationUnexpectedMerge:
		txt := "⚠ fusionnée directement dans " + d.Branch
		if d.Date != "" {
			txt += fmt.Sprintf(" (%s)", shortDate(d.Date))
		}
		return txt + " — hors GitFlow"
	case gitflow.DeviationUntagged:
		return fmt.Sprintf("⚠ fusionnée dans %s sans tag de version — hors GitFlow", d.Branch)
	case gitflow.DeviationStale:
		return fmt.Sprintf("⚠ aucun commit depuis %d jours — branche à finir ou à abandonner", d.Days)
	case gitflow.DeviationWrongParent:
		return fmt.Sprintf("⚠ semble être partie de %s plutôt que de %s — hors GitFlow", d.Branch, l.Node.Parent)
	}
	return ""
}

// nameText rend le nom d'une ligne, surligné si elle est sélectionnée.
func nameText(style lipgloss.Style, name string, selected bool) string {
	if selected {
		return selectionStyle(style.Faint(false), true).Render(name)
	}
	return style.Render(name)
}

// laneColor renvoie la couleur d'une ligne : celle de son type GitFlow, ou
// une couleur d'alerte pour une synchronisation develop → main, qui dévie du
// workflow GitFlow standard.
func laneColor(l graphLane) lipgloss.Color {
	if l.Kind == gitflow.LaneSync {
		return colorWarning
	}
	return colorFor(l.Node.Type)
}

func shortDate(iso string) string {
	if len(iso) >= 10 {
		return iso[:10]
	}
	return iso
}

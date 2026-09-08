package tui

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"gitflow-tui/internal/git"
	"gitflow-tui/internal/gitflow"
)

// cellWidth est la largeur en caractères d'une "colonne" chronologique dans
// le diagramme en couloirs horizontaux.
const cellWidth = 3

// laneLabelWidth est la largeur du préfixe (marqueur + nom de ligne) placé
// avant le corps du diagramme sur les lignes main/develop. Les lignes des
// branches éphémères démarrent leur propre indentation à la même colonne
// pour que leurs repères "└─▶" restent alignés avec les "┬" correspondants.
const laneLabelWidth = 11

// maxCommitsShown limite le nombre de commits listés sous chaque fusion.
const maxCommitsShown = 4

// branchMentionRe repère une mention de branche feature/release/hotfix dans
// un message de commit (message de fusion standard de git, ou mention dans
// un message de merge de pull request), pour retrouver une fusion même si la
// branche source a depuis été supprimée localement.
var branchMentionRe = regexp.MustCompile(`\b(?:feature|release|hotfix)/[A-Za-z0-9._-]+`)

// mergeRef décrit une fusion connue vers une cible : le commit de fusion
// (hash interne + date) quand il a pu être identifié dans l'historique, ou
// seulement la cible si on sait seulement que la branche y est intégrée
// (cas d'une fusion en fast-forward, sans commit de merge dédié). Le hash
// n'est jamais affiché tel quel, seulement utilisé pour retrouver les
// commits apportés par la fusion et repérer les échos transitifs.
type mergeRef struct {
	target string
	date   string
	hash   string
}

// nearestMatch renvoie la position, dans chain (une chaîne "premier parent"
// classée du plus récent au plus ancien), du premier commit appartenant à
// set — ou -1 si aucun ne correspond.
func nearestMatch(chain []string, set map[string]bool) int {
	for i, h := range chain {
		if set[h] {
			return i
		}
	}
	return -1
}

// isEcho signale que ev correspond en réalité à l'une des fusions de refs
// (même commit), et n'est donc pas une fusion distincte — utilisé pour ne
// pas confondre une intégration transitive (ex. main a intégré develop, qui
// contenait déjà la feature) avec une vraie fusion directe hors GitFlow.
func isEcho(ev mergeRef, refs []mergeRef) bool {
	if ev.hash == "" {
		return false
	}
	for _, r := range refs {
		if r.hash == ev.hash {
			return true
		}
	}
	return false
}

type laneKind int

const (
	laneLive    laneKind = iota // branche encore présente localement
	laneDeleted                 // branche reconstituée depuis l'historique, supprimée localement
	laneSync                    // fusion develop → main détectée dans l'historique
)

// timelineLane décrit une ligne du diagramme.
type timelineLane struct {
	node        gitflow.Node
	branch      git.Branch
	col         int
	merged      []mergeRef
	pending     []string
	anomalies   []mergeRef // fusions vers une cible non prévue par GitFlow pour ce type de branche
	wrongParent string     // non vide si la branche semble être partie de cette autre branche plutôt que de son parent attendu
	kind        laneKind
	commits     []git.Commit
}

// timeline est la représentation prête à être rendue : les deux lignes
// permanentes (main, develop), les éventuels commits directs sur celles-ci,
// et une ligne par branche ou synchronisation (locale ou reconstituée depuis
// l'historique).
type timeline struct {
	main          *timelineLane
	develop       *timelineLane
	mainDirect    []git.Commit
	developDirect []git.Commit
	lanes         []timelineLane
}

// scanMergeEvents parcourt l'historique atteignable depuis ref à la
// recherche de mentions de branches feature/release/hotfix dans les sujets
// de commit, et renvoie la première fusion trouvée pour chaque branche
// mentionnée.
func scanMergeEvents(repo git.Repository, ref string) map[string]mergeRef {
	events := make(map[string]mergeRef)
	out, err := repo.LogSubjects(ref)
	if err != nil {
		return events
	}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			continue
		}
		hash, date, subject := parts[0], parts[1], parts[2]
		name := branchMentionRe.FindString(subject)
		if name == "" {
			continue
		}
		if _, exists := events[name]; !exists {
			events[name] = mergeRef{target: ref, date: date, hash: hash}
		}
	}
	return events
}

// scanSyncEvents parcourt l'historique de mainRef à la recherche de commits
// de fusion de developName (avec ou sans suffixe "into <cible>"), pour
// retrouver chaque passage de develop vers main.
func scanSyncEvents(repo git.Repository, mainRef, developName string) []mergeRef {
	out, err := repo.LogSubjects(mainRef)
	if err != nil {
		return nil
	}
	prefix := "Merge branch '" + developName + "'"
	remotePrefix := "Merge remote-tracking branch 'origin/" + developName + "'"
	var events []mergeRef
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			continue
		}
		hash, date, subject := parts[0], parts[1], parts[2]
		if strings.HasPrefix(subject, prefix) || strings.HasPrefix(subject, remotePrefix) {
			events = append(events, mergeRef{target: mainRef, date: date, hash: hash})
		}
	}
	return events
}

// buildTimeline construit le diagramme à partir des branches classifiées.
// Chaque ligne (branche locale, branche reconstituée depuis l'historique, ou
// synchronisation develop → main) se voit attribuer une colonne unique
// d'après sa date, utilisée à la fois pour son propre repère et pour le
// repère correspondant sur la ou les lignes concernées.
func buildTimeline(repo git.Repository, nodes []gitflow.Node, byName map[string]git.Branch) timeline {
	var tl timeline
	var ephemeral []gitflow.Node

	for _, n := range nodes {
		switch n.Type {
		case gitflow.TypeMain:
			l := timelineLane{node: n, branch: byName[n.Name]}
			tl.main = &l
		case gitflow.TypeDevelop:
			l := timelineLane{node: n, branch: byName[n.Name]}
			tl.develop = &l
		case gitflow.TypeFeature, gitflow.TypeRelease, gitflow.TypeHotfix:
			ephemeral = append(ephemeral, n)
		}
	}

	mergesByTarget := make(map[string]map[string]mergeRef, 2)
	mainSpine := map[string]bool{}
	developSpine := map[string]bool{}
	if tl.main != nil {
		mergesByTarget[tl.main.node.Name] = scanMergeEvents(repo, tl.main.node.Name)
		if commits, err := repo.DirectCommits(tl.main.node.Name); err == nil {
			tl.mainDirect = commits
		}
		if hashes, err := repo.FirstParentHashes(tl.main.node.Name); err == nil {
			for _, h := range hashes {
				mainSpine[h] = true
			}
		}
	}
	if tl.develop != nil {
		mergesByTarget[tl.develop.node.Name] = scanMergeEvents(repo, tl.develop.node.Name)
		if commits, err := repo.DirectCommits(tl.develop.node.Name); err == nil {
			tl.developDirect = commits
		}
		if hashes, err := repo.FirstParentHashes(tl.develop.node.Name); err == nil {
			for _, h := range hashes {
				developSpine[h] = true
			}
		}
	}

	type candidate struct {
		node        gitflow.Node
		date        string
		kind        laneKind
		refs        []mergeRef // pré-calculées pour laneDeleted et laneSync
		anomalies   []mergeRef // pré-calculées pour laneDeleted
		wrongParent string     // renseigné si le point de divergence appartient en fait à l'autre ligne principale
	}

	var mainName, developName string
	if tl.main != nil {
		mainName = tl.main.node.Name
	}
	if tl.develop != nil {
		developName = tl.develop.node.Name
	}

	candidates := make([]candidate, 0, len(ephemeral))
	for _, n := range ephemeral {
		date := byName[n.Name].CommitDate
		if n.Parent != "" {
			if base, err := repo.MergeBase(n.Name, n.Parent); err == nil {
				if d, err := repo.CommitDate(base); err == nil {
					date = d
				}
			}
		}

		// Le vrai point de divergence de la branche est le premier commit de
		// sa propre ligne (premier parent) qui appartient aussi à l'une des
		// deux branches principales — pas un simple ancêtre commun lointain
		// (une fois la branche fusionnée partout, un merge-base avec
		// n'importe quelle branche renvoie trivialement sa propre pointe).
		// Si ce point est plus proche de la pointe côté de l'autre branche
		// principale que côté du parent attendu, la branche a probablement
		// été créée depuis cette autre branche.
		var wrongParent string
		var otherParent string
		var expectedSpine, otherSpine map[string]bool
		switch n.Parent {
		case developName:
			expectedSpine, otherSpine, otherParent = developSpine, mainSpine, mainName
		case mainName:
			expectedSpine, otherSpine, otherParent = mainSpine, developSpine, developName
		}
		if otherParent != "" {
			if chain, err := repo.FirstParentHashes(n.Name); err == nil {
				idxExpected := nearestMatch(chain, expectedSpine)
				idxOther := nearestMatch(chain, otherSpine)
				if idxOther >= 0 && (idxExpected < 0 || idxOther < idxExpected) {
					wrongParent = otherParent
				}
			}
		}

		candidates = append(candidates, candidate{node: n, date: date, wrongParent: wrongParent})
	}

	seen := make(map[string]bool, len(candidates))
	for name := range byName {
		seen[name] = true
	}
	mentioned := make(map[string]bool)
	for _, events := range mergesByTarget {
		for name := range events {
			mentioned[name] = true
		}
	}
	for name := range mentioned {
		if seen[name] {
			continue
		}
		seen[name] = true

		node := gitflow.Classify([]string{name})[0]
		validSet := make(map[string]bool, len(node.MergeTargets))
		for _, t := range node.MergeTargets {
			validSet[t] = true
		}

		// On ne retient comme fusion "normale" que les cibles réellement
		// valides pour ce type de branche (ex. develop pour une feature).
		var refs []mergeRef
		for _, target := range node.MergeTargets {
			if ev, ok := mergesByTarget[target][name]; ok {
				refs = append(refs, ev)
			}
		}

		// Une mention trouvée dans une cible non valide (ex. main pour une
		// feature) est soit un écho transitif (le commit est déjà compté
		// via refs — main a ensuite intégré develop), soit une vraie
		// anomalie : une fusion directe hors du flow attendu.
		var anomalies []mergeRef
		for target, events := range mergesByTarget {
			if validSet[target] {
				continue
			}
			if ev, ok := events[name]; ok && !isEcho(ev, refs) {
				anomalies = append(anomalies, ev)
			}
		}

		if len(refs) == 0 && len(anomalies) == 0 {
			continue
		}
		sort.Slice(refs, func(i, j int) bool { return refs[i].date < refs[j].date })
		sort.Slice(anomalies, func(i, j int) bool { return anomalies[i].date < anomalies[j].date })

		date := ""
		if len(refs) > 0 {
			date = refs[0].date
		}
		if len(anomalies) > 0 && (date == "" || anomalies[0].date < date) {
			date = anomalies[0].date
		}

		candidates = append(candidates, candidate{node: node, date: date, kind: laneDeleted, refs: refs, anomalies: anomalies})
	}

	if tl.main != nil && tl.develop != nil {
		for _, ev := range scanSyncEvents(repo, tl.main.node.Name, tl.develop.node.Name) {
			node := gitflow.Node{
				Name:   tl.develop.node.Name + " → " + tl.main.node.Name,
				Type:   gitflow.TypeOther,
				Parent: tl.develop.node.Name,
			}
			candidates = append(candidates, candidate{node: node, date: ev.date, kind: laneSync, refs: []mergeRef{ev}})
		}
	}

	sort.Slice(candidates, func(i, j int) bool { return candidates[i].date < candidates[j].date })

	for i, c := range candidates {
		lane := timelineLane{node: c.node, col: i, kind: c.kind}
		switch c.kind {
		case laneDeleted, laneSync:
			lane.merged = c.refs
			lane.anomalies = c.anomalies
			hash := ""
			if len(c.refs) > 0 {
				hash = c.refs[0].hash
			} else if len(c.anomalies) > 0 {
				hash = c.anomalies[0].hash
			}
			if hash != "" {
				if commits, err := repo.MergeCommits(hash); err == nil {
					lane.commits = commits
				}
			}
		default:
			lane.branch = byName[c.node.Name]
			lane.wrongParent = c.wrongParent
			validSet := make(map[string]bool, len(c.node.MergeTargets))
			for _, target := range c.node.MergeTargets {
				validSet[target] = true
				if ev, ok := mergesByTarget[target][c.node.Name]; ok {
					lane.merged = append(lane.merged, ev)
				} else if merged, err := repo.IsAncestor(c.node.Name, target); err == nil && merged {
					lane.merged = append(lane.merged, mergeRef{target: target})
				} else {
					lane.pending = append(lane.pending, target)
				}
			}
			for target, events := range mergesByTarget {
				if validSet[target] {
					continue
				}
				if ev, ok := events[c.node.Name]; ok && !isEcho(ev, lane.merged) {
					lane.anomalies = append(lane.anomalies, ev)
				}
			}
			if commits, err := repo.CommitsNotIn(c.node.Name, c.node.Parent); err == nil {
				lane.commits = commits
			}
		}
		tl.lanes = append(tl.lanes, lane)
	}

	return tl
}

// laneBelongsToMain indique si une ligne doit être regroupée sous la ligne
// main plutôt que sous develop. Le critère est la cible de fusion attendue
// pour le type de branche, pas son origine : une release part de develop
// mais son but est d'atterrir dans main, donc elle va avec main — comme les
// hotfix et les synchronisations develop → main. Seule feature, qui ne doit
// fusionner que dans develop, va avec develop.
func laneBelongsToMain(l timelineLane) bool {
	switch l.node.Type {
	case gitflow.TypeRelease, gitflow.TypeHotfix:
		return true
	case gitflow.TypeFeature:
		return false
	default:
		return l.kind == laneSync
	}
}

// filterLive réduit un diagramme aux seules branches actuellement présentes
// localement et pas encore complètement fusionnées vers toutes leurs
// cibles : le travail réellement en cours à l'instant présent. Exclut donc
// les branches supprimées reconstituées depuis l'historique, les
// synchronisations minées dans les messages de commit, les commits directs
// hors fusion (détectés en parcourant tout l'historique), et les fusions
// hors flow détectées via mention dans un message de commit — toutes des
// alertes qui relèvent de l'historique, pas de l'état courant.
func filterLive(tl timeline) timeline {
	out := tl
	out.mainDirect = nil
	out.developDirect = nil
	out.lanes = nil
	for _, l := range tl.lanes {
		if l.kind != laneLive || len(l.pending) == 0 {
			continue
		}
		l.anomalies = nil
		out.lanes = append(out.lanes, l)
	}
	return out
}

func renderTimeline(tl timeline, emptyMsg string) string {
	if tl.main == nil && tl.develop == nil {
		return "Aucune branche main/develop trouvée."
	}

	maxCol := -1
	for _, l := range tl.lanes {
		if l.col > maxCol {
			maxCol = l.col
		}
	}
	spineLen := (maxCol+2)*cellWidth + 2
	if spineLen < 16 {
		spineLen = 16
	}

	var mainLanes, developLanes []timelineLane
	for _, l := range tl.lanes {
		if laneBelongsToMain(l) {
			mainLanes = append(mainLanes, l)
		} else {
			developLanes = append(developLanes, l)
		}
	}

	var b strings.Builder
	writeGroup := func(label string, color lipgloss.Color, spine *timelineLane, direct []git.Commit, group []timelineLane) {
		if spine == nil {
			return
		}
		b.WriteString(renderSpine(label, color, *spine, spineLen, tl.lanes))
		b.WriteString("\n")
		if block := renderDirectCommits(label, direct); block != "" {
			b.WriteString(block)
			b.WriteString("\n")
		}
		b.WriteString("\n")
		for _, l := range group {
			b.WriteString(renderLane(l))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	writeGroup("main", colorMain, tl.main, tl.mainDirect, mainLanes)
	writeGroup("develop", colorDevelop, tl.develop, tl.developDirect, developLanes)

	if len(tl.lanes) == 0 {
		b.WriteString(styleFaint.Render(emptyMsg))
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

// renderDirectCommits affiche, juste sous une ligne permanente, les commits
// qui y ont été committés directement plutôt que d'y arriver par une fusion
// — un contournement complet du workflow GitFlow.
func renderDirectCommits(spineLabel string, commits []git.Commit) string {
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
		lines = append(lines, commitStyle.Render(fmt.Sprintf("%s    · %s %s", indent, c.Hash, c.Subject)))
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
// atterri dans main, conforme ou non. Les repères des branches supprimées
// sont atténués.
func renderSpine(label string, spineColor lipgloss.Color, spine timelineLane, length int, lanes []timelineLane) string {
	cells := make([]rune, length)
	for i := range cells {
		cells[i] = '─'
	}
	marks := make(map[int]lipgloss.Color, len(lanes))
	faint := make(map[int]bool, len(lanes))
	for _, l := range lanes {
		concerned := l.node.Parent == spine.node.Name
		anomaly := false
		if !concerned {
			for _, mr := range l.merged {
				if mr.target == spine.node.Name {
					concerned = true
					break
				}
			}
		}
		if !concerned {
			for _, mr := range l.anomalies {
				if mr.target == spine.node.Name {
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
			faint[pos] = l.kind == laneDeleted
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
	if spine.branch.IsHead {
		marker = "●"
	}
	head := lipgloss.NewStyle().Foreground(spineColor).Bold(true).Render(fmt.Sprintf("%s %-9s", marker, label))
	return head + body.String()
}

// renderLane dessine la ligne d'une branche ou d'une synchronisation : un
// repère aligné sous celui de sa ligne parente, son nom, ses cibles de
// fusion, son statut, une éventuelle fusion hors GitFlow, et la liste
// (tronquée) des commits apportés. Les branches supprimées sont affichées
// en atténué avec la mention "supprimée" ; toute fusion en dehors du flow
// GitFlow attendu (develop → main direct, feature → main, fusion
// incomplète...) est signalée en couleur d'alerte.
func renderLane(l timelineLane) string {
	color := laneColor(l)
	indent := strings.Repeat(" ", laneLabelWidth)
	subIndent := indent + "    "
	faint := l.kind == laneDeleted
	warnStyle := lipgloss.NewStyle().Foreground(colorWarning)

	connStyle := lipgloss.NewStyle().Foreground(color).Faint(faint)
	connector := connStyle.Render("└─▶")

	nameStyle := lipgloss.NewStyle().Foreground(color).Faint(faint)
	if l.kind == laneLive && l.branch.IsHead {
		nameStyle = nameStyle.Bold(true)
	}

	marker := " "
	switch {
	case l.kind == laneDeleted:
		marker = "×"
	case l.kind == laneSync:
		marker = "⚠"
	case l.branch.IsHead:
		marker = "●"
	}

	var parts []string
	for _, mr := range l.merged {
		if mr.date != "" {
			parts = append(parts, fmt.Sprintf("%s (%s)", mr.target, shortDate(mr.date)))
		} else {
			parts = append(parts, mr.target)
		}
	}
	for _, t := range l.pending {
		parts = append(parts, warnStyle.Render(t+" (en attente)"))
	}
	targetTxt := "—"
	if len(parts) > 0 {
		targetTxt = strings.Join(parts, ", ")
	}

	var status string
	switch {
	case l.kind == laneDeleted:
		status = "✔ fusionnée (supprimée)"
	case l.kind == laneSync:
		status = warnStyle.Render("⚠ fusion directe (hors GitFlow)")
	case len(l.pending) == 0 && len(l.merged) > 0:
		status = "✔ fusionnée"
	case len(l.merged) > 0:
		status = warnStyle.Render("⚠ partiellement fusionnée")
	default:
		status = "○ en cours"
	}

	lines := []string{fmt.Sprintf("%s%s %s%s  → %s   %s",
		indent, connector, marker, nameStyle.Render(l.node.Name), targetTxt, status)}

	for _, mr := range l.anomalies {
		txt := fmt.Sprintf("⚠ fusionnée directement dans %s", mr.target)
		if mr.date != "" {
			txt += fmt.Sprintf(" (%s)", shortDate(mr.date))
		}
		txt += " — hors GitFlow"
		lines = append(lines, subIndent+warnStyle.Render(txt))
	}

	if l.wrongParent != "" {
		txt := fmt.Sprintf("⚠ semble être partie de %s plutôt que de %s — hors GitFlow", l.wrongParent, l.node.Parent)
		lines = append(lines, subIndent+warnStyle.Render(txt))
	}

	if len(l.commits) > 0 {
		commitStyle := lipgloss.NewStyle().Faint(true)
		shown := l.commits
		more := 0
		if len(shown) > maxCommitsShown {
			more = len(shown) - maxCommitsShown
			shown = shown[:maxCommitsShown]
		}
		for _, c := range shown {
			lines = append(lines, commitStyle.Render(fmt.Sprintf("%s· %s %s", subIndent, c.Hash, c.Subject)))
		}
		if more > 0 {
			lines = append(lines, commitStyle.Render(fmt.Sprintf("%s… +%d commit(s)", subIndent, more)))
		}
	}

	return strings.Join(lines, "\n")
}

// laneColor renvoie la couleur d'une ligne : celle de son type GitFlow, ou
// une couleur d'alerte pour une synchronisation develop → main, qui dévie du
// workflow GitFlow standard.
func laneColor(l timelineLane) lipgloss.Color {
	if l.kind == laneSync {
		return colorWarning
	}
	return colorFor(l.node.Type)
}

func shortDate(iso string) string {
	if len(iso) >= 10 {
		return iso[:10]
	}
	return iso
}

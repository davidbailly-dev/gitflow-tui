package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"gitflow-tui/internal/git"
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
	tag    string // tag de version posé sur cette fusion (cf. checkVersionTags)
}

// mergeIndex recense, pour chaque branche permanente (cible), les fusions de
// branches éphémères trouvées dans son historique : cible → branche source
// → fusions, de la plus récente à la plus ancienne. Une branche peut avoir
// été fusionnée plusieurs fois (travail repris après une première fusion).
type mergeIndex map[string]map[string][]mergeRef

// latest renvoie la fusion la plus récente de name trouvée dans target.
func (idx mergeIndex) latest(target, name string) (mergeRef, bool) {
	events := idx[target][name]
	if len(events) == 0 {
		return mergeRef{}, false
	}
	return events[0], true
}

// unexpected renvoie, pour chaque cible non prévue par GitFlow pour node, la
// fusion la plus récente de node trouvée dans son historique qui ne soit pas
// un simple écho d'une fusion vers une cible valide — même commit, devenu
// atteignable depuis main parce que main a ensuite intégré develop ou une
// release. Toutes les fusions vers les cibles valides sont prises en compte
// (pas seulement la plus récente) : une feature fusionnée deux fois dans
// develop, dont seule la première fusion a déjà atteint main, n'est pas une
// anomalie.
func (idx mergeIndex) unexpected(node gitflow.Node) []mergeRef {
	valid := make(map[string]bool, len(node.MergeTargets))
	expected := make(map[string]bool)
	for _, target := range node.MergeTargets {
		valid[target] = true
		for _, ev := range idx[target][node.Name] {
			expected[ev.hash] = true
		}
	}
	var out []mergeRef
	for target, byName := range idx {
		if valid[target] {
			continue
		}
		for _, ev := range byName[node.Name] {
			if !expected[ev.hash] {
				out = append(out, ev)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].date < out[j].date })
	return out
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
	date        string // position sur l'axe du temps, cf. laneDate
	// untaggedIn nomme la branche principale quand la fusion de cette
	// release ou de ce hotfix n'y porte pas de tag de version, dans un
	// dépôt qui étiquette pourtant ses versions ; vide sinon.
	untaggedIn string
	// staleDays est le nombre de jours sans commit d'une branche pas encore
	// complètement fusionnée, au-delà du seuil d'inactivité ; 0 sinon.
	staleDays int
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
	// unreleased est le nombre de commits (hors fusions) de develop pas
	// encore arrivés dans main : le contenu de la prochaine release.
	unreleased int
	lanes      []timelineLane
}

// timelineSources rassemble ce dont buildTimeline a besoin : le dépôt, ses
// conventions GitFlow, et les données déjà chargées pour la vue colonnes.
type timelineSources struct {
	repo       git.Repository
	classifier gitflow.Classifier
	nodes      []gitflow.Node
	byName     map[string]git.Branch
	spines     spines
	commits    map[string][]git.Commit // commits propres de chaque branche locale
	staleAfter time.Duration           // 0 : pas de détection d'inactivité
	now        time.Time
}

// foundMerge associe une fusion lue dans un message de commit au commit
// qui la porte.
type foundMerge struct {
	merge gitflow.Merge
	ref   mergeRef
}

// scanMerges parcourt les commits de fusion atteignables depuis ref et
// renvoie ceux dont le message désigne une branche source reconnue par
// keep, du plus récent au plus ancien.
func scanMerges(repo git.Repository, ref string, keep func(gitflow.Merge) bool) []foundMerge {
	var out []foundMerge
	subjects, err := repo.MergeSubjects(ref)
	if err != nil || subjects == "" {
		return out
	}
	for _, line := range strings.Split(subjects, "\n") {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			continue
		}
		merge, ok := gitflow.ParseMerge(parts[2])
		if !ok || merge.IsPull() || !keep(merge) {
			continue
		}
		out = append(out, foundMerge{merge: merge, ref: mergeRef{target: ref, date: parts[1], hash: parts[0]}})
	}
	return out
}

// scanMergeEvents recense les fusions de branches feature/release/hotfix
// atteignables depuis ref, groupées par branche source.
func scanMergeEvents(repo git.Repository, classifier gitflow.Classifier, ref string) map[string][]mergeRef {
	events := make(map[string][]mergeRef)
	ephemeral := func(m gitflow.Merge) bool { return classifier.ClassifyOne(m.Source).Type.IsEphemeral() }
	for _, found := range scanMerges(repo, ref, ephemeral) {
		events[found.merge.Source] = append(events[found.merge.Source], found.ref)
	}
	return events
}

// scanSyncEvents parcourt l'historique de mainRef à la recherche des
// fusions de developName dans mainRef (message git standard, ou merge de
// pull request), pour retrouver chaque passage de develop vers main. Les
// fusions de develop vers une autre branche (ex. une feature mise à jour
// depuis develop), également atteignables depuis main, sont écartées.
func scanSyncEvents(repo git.Repository, mainRef, developName string) []mergeRef {
	sync := func(m gitflow.Merge) bool {
		return m.Source == developName && (m.Target == "" || m.Target == mainRef)
	}
	var events []mergeRef
	for _, found := range scanMerges(repo, mainRef, sync) {
		events = append(events, found.ref)
	}
	return events
}

// buildTimeline construit le diagramme à partir des branches classifiées.
// Chaque ligne (branche locale, branche reconstituée depuis l'historique, ou
// synchronisation develop → main) est datée selon une même règle (cf.
// laneDate), qui fixe sa colonne sur l'axe du temps commun aux deux lignes
// permanentes.
func buildTimeline(src timelineSources) timeline {
	repo, classifier, byName := src.repo, src.classifier, src.byName
	var tl timeline
	var ephemeral []gitflow.Node

	for _, n := range src.nodes {
		switch {
		case n.Type == gitflow.TypeMain:
			l := timelineLane{node: n, branch: byName[n.Name]}
			tl.main = &l
		case n.Type == gitflow.TypeDevelop:
			l := timelineLane{node: n, branch: byName[n.Name]}
			tl.develop = &l
		case n.Type.IsEphemeral():
			ephemeral = append(ephemeral, n)
		}
	}
	if tl.main != nil && tl.develop != nil {
		tl.unreleased, _ = repo.CountNotIn(tl.develop.node.Name, tl.main.node.Name)
	}

	mergesByTarget := make(mergeIndex, 2)
	if tl.main != nil {
		mergesByTarget[tl.main.node.Name] = scanMergeEvents(repo, classifier, tl.main.node.Name)
		tl.mainDirect = directCommits(repo, tl.main.node.Name)
	}
	if tl.develop != nil {
		mergesByTarget[tl.develop.node.Name] = scanMergeEvents(repo, classifier, tl.develop.node.Name)
		tl.developDirect = directCommits(repo, tl.develop.node.Name)
	}

	var lanes []timelineLane
	for _, n := range ephemeral {
		lane := liveLane(repo, classifier, n, byName[n.Name], src.commits[n.Name], mergesByTarget, src.spines)
		lane.staleDays = staleDays(lane, src.staleAfter, src.now)
		lanes = append(lanes, lane)
	}

	seen := make(map[string]bool, len(byName))
	for name := range byName {
		seen[name] = true
	}
	for _, bySource := range mergesByTarget {
		for name := range bySource {
			if seen[name] {
				continue
			}
			seen[name] = true
			if lane, ok := deletedLane(repo, classifier.ClassifyOne(name), mergesByTarget); ok {
				lanes = append(lanes, lane)
			}
		}
	}

	if tl.main != nil && tl.develop != nil {
		for _, ev := range scanSyncEvents(repo, tl.main.node.Name, tl.develop.node.Name) {
			node := gitflow.Node{
				Name:   tl.develop.node.Name + " → " + tl.main.node.Name,
				Type:   gitflow.TypeOther,
				Parent: tl.develop.node.Name,
			}
			lane := timelineLane{node: node, kind: laneSync, merged: []mergeRef{ev}, date: ev.date}
			if commits, err := repo.MergeCommits(ev.hash); err == nil {
				lane.commits = commits
			}
			lanes = append(lanes, lane)
		}
	}

	if tl.main != nil {
		checkVersionTags(repo, classifier, tl.main.node.Name, lanes)
	}

	sort.Slice(lanes, func(i, j int) bool {
		if lanes[i].date != lanes[j].date {
			return lanes[i].date < lanes[j].date
		}
		return lanes[i].node.Name < lanes[j].node.Name
	})
	for i := range lanes {
		lanes[i].col = i
	}
	tl.lanes = lanes
	return tl
}

// staleDays renvoie le nombre de jours écoulés depuis le dernier commit
// d'une branche locale pas encore complètement fusionnée, s'il dépasse
// staleAfter ; 0 sinon (seuil nul, branche fusionnée, date illisible).
func staleDays(l timelineLane, staleAfter time.Duration, now time.Time) int {
	if staleAfter <= 0 || len(l.pending) == 0 {
		return 0
	}
	last, err := time.Parse("2006-01-02 15:04:05 -0700", l.branch.CommitDate)
	if err != nil {
		return 0
	}
	idle := now.Sub(last)
	if idle < staleAfter {
		return 0
	}
	return maxInt(int(idle.Hours()/24), 1)
}

// checkVersionTags retrouve le tag de version de chaque fusion de release
// ou de hotfix dans mainName : posé sur le commit de fusion (git flow), ou
// à défaut sur la pointe de la branche fusionnée. Une fusion sans tag est
// signalée (untaggedIn) — sauf si le dépôt n'a aucun tag de version, signe
// qu'il ne suit pas cette convention.
func checkVersionTags(repo git.Repository, classifier gitflow.Classifier, mainName string, lanes []timelineLane) {
	all, err := repo.Tags()
	if err != nil {
		return
	}
	tags := make(map[string][]string, len(all))
	for commit, names := range all {
		for _, name := range names {
			if classifier.Config.IsVersionTag(name) {
				tags[commit] = append(tags[commit], name)
			}
		}
	}
	if len(tags) == 0 {
		return
	}
	for i := range lanes {
		l := &lanes[i]
		if l.node.Type != gitflow.TypeRelease && l.node.Type != gitflow.TypeHotfix {
			continue
		}
		for j := range l.merged {
			mr := &l.merged[j]
			if mr.target != mainName || mr.hash == "" {
				continue
			}
			found := tags[mr.hash]
			if len(found) == 0 {
				if tip, err := repo.ResolveCommit(mr.hash + "^2"); err == nil {
					found = tags[tip]
				}
			}
			if len(found) > 0 {
				mr.tag = found[0]
			} else {
				l.untaggedIn = mainName
			}
		}
	}
}

// directCommits renvoie les commits arrivés directement sur ref, hors
// fusion. Les intégrations de pull request en squash, qui n'ont qu'un parent
// mais sont bien passées par une revue, n'en font pas partie.
func directCommits(repo git.Repository, ref string) []git.Commit {
	commits, err := repo.DirectCommits(ref)
	if err != nil {
		return nil
	}
	out := commits[:0]
	for _, c := range commits {
		if !gitflow.IsSquashMerge(c.Subject) {
			out = append(out, c)
		}
	}
	return out
}

// laneDate renvoie la position d'une ligne sur l'axe du temps : la date de
// sa première intégration connue (fusion vers une cible valide ou non),
// sinon fallback — le point de divergence d'une branche pas encore
// fusionnée. Toutes les dates sont au même format (ISO, fuseau local) et se
// comparent donc comme des chaînes.
func laneDate(merged, anomalies []mergeRef, fallback string) string {
	date := ""
	for _, refs := range [][]mergeRef{merged, anomalies} {
		for _, mr := range refs {
			if mr.date != "" && (date == "" || mr.date < date) {
				date = mr.date
			}
		}
	}
	if date == "" {
		return fallback
	}
	return date
}

// liveLane construit la ligne d'une branche éphémère encore présente
// localement : fusions vers chacune de ses cibles (identifiées par leur
// commit de fusion, ou à défaut par ascendance), cibles en attente,
// fusions hors flow et point de divergence.
func liveLane(repo git.Repository, classifier gitflow.Classifier, n gitflow.Node, branch git.Branch, commits []git.Commit, merges mergeIndex, sp spines) timelineLane {
	lane := timelineLane{node: n, branch: branch, kind: laneLive, commits: commits}
	for _, target := range n.MergeTargets {
		if ev, ok := merges.latest(target, n.Name); ok {
			lane.merged = append(lane.merged, ev)
			continue
		}
		// Une branche sans commit propre (tout juste créée) est trivialement
		// contenue dans sa parente : ce n'est pas pour autant une fusion.
		if len(commits) > 0 {
			if merged, err := repo.IsAncestor(n.Name, target); err == nil && merged {
				lane.merged = append(lane.merged, mergeRef{target: target})
				continue
			}
		}
		lane.pending = append(lane.pending, target)
	}
	lane.anomalies = merges.unexpected(n)

	fork, wrongParent := divergence(repo, n, classifier, sp)
	lane.wrongParent = wrongParent
	fallback := branch.CommitDate
	if fork != "" {
		if d, err := repo.CommitDate(fork); err == nil {
			fallback = d
		}
	}
	lane.date = laneDate(lane.merged, lane.anomalies, fallback)
	return lane
}

// deletedLane reconstitue, depuis les messages de fusion, la ligne d'une
// branche éphémère supprimée localement. ok vaut false si aucune fusion de
// la branche n'a été trouvée.
func deletedLane(repo git.Repository, node gitflow.Node, merges mergeIndex) (timelineLane, bool) {
	// On ne retient comme fusion "normale" que les cibles réellement
	// valides pour ce type de branche (ex. develop pour une feature).
	var refs []mergeRef
	for _, target := range node.MergeTargets {
		if ev, ok := merges.latest(target, node.Name); ok {
			refs = append(refs, ev)
		}
	}

	// Une fusion trouvée dans une cible non valide (ex. main pour une
	// feature) est soit un écho transitif (même commit que l'une des fusions
	// valides — main a ensuite intégré develop), soit une vraie anomalie :
	// une fusion directe hors du flow attendu.
	anomalies := merges.unexpected(node)

	if len(refs) == 0 && len(anomalies) == 0 {
		return timelineLane{}, false
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].date < refs[j].date })

	// Première fusion réellement identifiée : ses commits sont ceux de la
	// branche, et son second parent est la pointe de la branche supprimée.
	known := refs
	if len(known) == 0 {
		known = anomalies
	}
	hash := known[0].hash

	// Une cible valide sans fusion identifiée peut malgré tout contenir le
	// travail de la branche, arrivé par un autre chemin (hotfix fusionné
	// dans la release en cours, qui a ensuite rejoint develop) : il suffit
	// de vérifier que la cible contient la pointe de la branche. Sinon la
	// cible n'a jamais été atteinte.
	var pending []string
	for _, target := range node.MergeTargets {
		if _, ok := merges.latest(target, node.Name); ok {
			continue
		}
		if merged, err := repo.IsAncestor(hash+"^2", target); err == nil && merged {
			refs = append(refs, mergeRef{target: target})
			continue
		}
		pending = append(pending, target)
	}

	lane := timelineLane{
		node:      node,
		kind:      laneDeleted,
		merged:    refs,
		anomalies: anomalies,
		pending:   pending,
		date:      laneDate(refs, anomalies, ""),
	}
	if commits, err := repo.MergeCommits(hash); err == nil {
		lane.commits = commits
	}
	return lane, true
}

// laneBelongsToMain indique si une ligne doit être regroupée sous la ligne
// main plutôt que sous develop. Le critère est la cible de fusion attendue
// pour le type de branche, pas son origine : une release part de develop
// mais son but est d'atterrir dans main, donc elle va avec main — comme les
// hotfix et les synchronisations develop → main. Seules feature et bugfix,
// qui ne doivent fusionner que dans develop, vont avec develop.
func laneBelongsToMain(l timelineLane) bool {
	switch l.node.Type {
	case gitflow.TypeRelease, gitflow.TypeHotfix:
		return true
	case gitflow.TypeFeature, gitflow.TypeBugfix:
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

// laneHasAlert indique si une ligne signale une déviation du workflow
// GitFlow standard : fusion hors flow attendu pour ce type de branche,
// branche apparemment partie de la mauvaise ligne principale,
// synchronisation directe develop → main, fusion partielle (certaines
// cibles atteintes, d'autres non), fusion dans main sans tag de version, ou
// branche inactive depuis trop longtemps.
func laneHasAlert(l timelineLane) bool {
	if len(l.anomalies) > 0 || l.wrongParent != "" || l.kind == laneSync || l.untaggedIn != "" || l.staleDays > 0 {
		return true
	}
	return len(l.merged) > 0 && len(l.pending) > 0
}

// filterAlerts réduit un diagramme complet aux seules lignes qui signalent
// une déviation du workflow GitFlow standard, pour se concentrer sur ce qui
// mérite attention plutôt que sur l'historique conforme. Les commits
// directs sur main/develop sont conservés tels quels : par nature, ils
// constituent déjà une alerte.
func filterAlerts(tl timeline) timeline {
	out := tl
	out.lanes = nil
	for _, l := range tl.lanes {
		if laneHasAlert(l) {
			out.lanes = append(out.lanes, l)
		}
	}
	return out
}

// compactColumns renumérote les colonnes des lignes visibles de 0 à n-1,
// dans leur ordre chronologique : une fois l'historique filtré (alertes,
// direct), les colonnes des lignes masquées laisseraient sinon des trous et
// étireraient le diagramme bien au-delà de ce qui est affiché.
func compactColumns(lanes []timelineLane) []timelineLane {
	out := make([]timelineLane, len(lanes))
	copy(out, lanes)
	sort.SliceStable(out, func(i, j int) bool { return out[i].col < out[j].col })
	for i := range out {
		out[i].col = i
	}
	return out
}

// laneX renvoie la position horizontale (en caractères) du repère d'une
// ligne, identique sur la ligne permanente ("┬") et sur la ligne de la
// branche ("└─▶").
func laneX(l timelineLane) int {
	return laneLabelWidth + l.col*cellWidth
}

// rails dessine les width premiers caractères d'une ligne du diagramme : un
// trait vertical "│" sous le repère de chaque ligne encore à venir (open),
// qui relie ce repère sur la ligne permanente à la ligne de sa branche, et
// des espaces ailleurs.
func rails(width int, open []timelineLane) string {
	at := make(map[int]timelineLane, len(open))
	for _, l := range open {
		at[laneX(l)] = l
	}
	var b strings.Builder
	for x := 0; x < width; x++ {
		if l, ok := at[x]; ok {
			b.WriteString(lipgloss.NewStyle().Foreground(laneColor(l)).Faint(l.kind == laneDeleted).Render("│"))
		} else {
			b.WriteByte(' ')
		}
	}
	return b.String()
}

// renderTimeline dessine le diagramme : chaque ligne permanente, suivie des
// lignes de branches qui s'y rattachent, de la plus récente (repère le plus
// à droite) à la plus ancienne. Dans cet ordre, les traits verticaux des
// lignes restant à dessiner passent toujours à gauche du texte de la ligne
// courante, sans jamais le croiser.
//
// Il renvoie aussi, dans l'ordre d'affichage, la position (numéro de ligne)
// de chaque ligne de branche, pour la sélection au clavier ; selectedKey
// désigne la ligne sélectionnée (cf. laneKey), dont le nom est surligné.
func renderTimeline(tl timeline, emptyMsg, selectedKey string) (string, []graphAnchor) {
	if tl.main == nil && tl.develop == nil {
		return "Aucune branche main/develop trouvée.", nil
	}

	tl.lanes = compactColumns(tl.lanes)
	spineLen := (len(tl.lanes)+1)*cellWidth + 2
	if spineLen < 16 {
		spineLen = 16
	}

	var mainLanes, developLanes []timelineLane
	for i := len(tl.lanes) - 1; i >= 0; i-- {
		if laneBelongsToMain(tl.lanes[i]) {
			mainLanes = append(mainLanes, tl.lanes[i])
		} else {
			developLanes = append(developLanes, tl.lanes[i])
		}
	}

	var b strings.Builder
	var anchors []graphAnchor
	if axis := renderAxis(tl.lanes); axis != "" {
		b.WriteString(axis)
		b.WriteString("\n")
	}

	writeGroup := func(color lipgloss.Color, spine *timelineLane, direct []git.Commit, group []timelineLane) {
		if spine == nil {
			return
		}
		b.WriteString(renderSpine(color, *spine, spineLen, tl.lanes, group))
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
		if spine == tl.develop && tl.unreleased > 0 && tl.main != nil {
			b.WriteString(strings.Repeat(" ", laneLabelWidth))
			b.WriteString(styleFaint.Render(fmt.Sprintf("↑ %d commit(s) pas encore dans %s — contenu de la prochaine release", tl.unreleased, tl.main.node.Name)))
			b.WriteString("\n")
		}
		if block := renderDirectCommits(spine.node.Name, direct); block != "" {
			b.WriteString(block)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	writeGroup(colorMain, tl.main, tl.mainDirect, mainLanes)
	writeGroup(colorDevelop, tl.develop, tl.developDirect, developLanes)

	if len(tl.lanes) == 0 && len(tl.mainDirect) == 0 && len(tl.developDirect) == 0 {
		b.WriteString(styleFaint.Render(emptyMsg))
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n"), anchors
}

// graphAnchor situe une ligne de branche dans le rendu du graphe.
type graphAnchor struct {
	line int
	lane timelineLane
}

// laneKey identifie une ligne du graphe d'un rendu à l'autre, pour garder
// la sélection après un rechargement ou un changement de mode (plusieurs
// synchronisations develop → main portent le même nom, d'où la date).
func laneKey(l timelineLane) string {
	return fmt.Sprintf("%d|%s|%s", l.kind, l.node.Name, l.date)
}

// renderAxis dessine l'axe du temps, au-dessus des lignes permanentes :
// l'année dans la marge (celle de la première date), puis la date (MM-JJ)
// de chaque jour sous la première colonne de ce jour, alignée sur son
// repère "┬" — ou la date complète quand l'année change. Une date qui
// chevaucherait la précédente est omise plutôt que décalée sous une autre
// colonne, où elle serait trompeuse. lanes doit être trié par colonne.
func renderAxis(lanes []timelineLane) string {
	var b strings.Builder
	end := 0 // première position libre après le dernier texte écrit
	prevDay, year := "", ""
	for _, l := range lanes {
		if len(l.date) < 10 || l.date[:10] == prevDay {
			continue
		}
		day := l.date[:10]
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
// atterri dans main, conforme ou non. Les lignes de group, dessinées sous
// cette ligne permanente, y ont toujours un repère, point de départ de leur
// trait vertical. Les repères des branches supprimées sont atténués.
func renderSpine(spineColor lipgloss.Color, spine timelineLane, length int, lanes, group []timelineLane) string {
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
		concerned := inGroup[l.col] || l.node.Parent == spine.node.Name
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
	// Nom tronqué pour ne pas décaler l'axe du temps (cf. laneLabelWidth),
	// en gardant une espace avant le trait.
	name := ansi.Truncate(spine.node.Name, laneLabelWidth-3, "…")
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
func renderLane(l timelineLane, open []timelineLane, selected bool) string {
	color := laneColor(l)
	indent := rails(laneX(l), open)
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
		part := mr.target
		if mr.date != "" {
			part += fmt.Sprintf(" (%s)", shortDate(mr.date))
		}
		if mr.tag != "" {
			part += " " + lipgloss.NewStyle().Foreground(colorRelease).Render("◆ "+mr.tag)
		}
		parts = append(parts, part)
	}
	// Une branche supprimée ne sera plus fusionnée : sa cible manquante
	// n'est pas "en attente" mais définitivement ratée.
	pendingLabel := " (en attente)"
	if l.kind == laneDeleted {
		pendingLabel = " (jamais fusionnée)"
	}
	for _, t := range l.pending {
		parts = append(parts, warnStyle.Render(t+pendingLabel))
	}
	targetTxt := "—"
	if len(parts) > 0 {
		targetTxt = strings.Join(parts, ", ")
	}

	var status string
	switch {
	case l.kind == laneDeleted && len(l.pending) == 0:
		status = "✔ fusionnée (supprimée)"
	case l.kind == laneDeleted && len(l.merged) > 0:
		status = warnStyle.Render("⚠ partiellement fusionnée (supprimée)")
	case l.kind == laneDeleted:
		status = "× supprimée"
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
		indent, connector, marker, nameText(nameStyle, l.node.Name, selected), targetTxt, status)}

	for _, mr := range l.anomalies {
		txt := fmt.Sprintf("⚠ fusionnée directement dans %s", mr.target)
		if mr.date != "" {
			txt += fmt.Sprintf(" (%s)", shortDate(mr.date))
		}
		txt += " — hors GitFlow"
		lines = append(lines, subIndent+warnStyle.Render(txt))
	}

	if l.untaggedIn != "" {
		lines = append(lines, subIndent+warnStyle.Render(fmt.Sprintf("⚠ fusionnée dans %s sans tag de version — hors GitFlow", l.untaggedIn)))
	}

	if l.staleDays > 0 {
		lines = append(lines, subIndent+warnStyle.Render(fmt.Sprintf("⚠ aucun commit depuis %d jours — branche à finir ou à abandonner", l.staleDays)))
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

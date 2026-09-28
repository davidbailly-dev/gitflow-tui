package gitflow

import (
	"runtime"
	"sort"
	"sync"
	"time"
)

// Options règle l'analyse.
type Options struct {
	// StaleAfter est la durée sans commit au-delà de laquelle une branche
	// éphémère pas encore complètement fusionnée est signalée comme
	// inactive ; 0 désactive ce signalement.
	StaleAfter time.Duration
	// Now est l'instant de référence de l'analyse (inactivité).
	Now time.Time
}

// Analyze lit l'historique du dépôt et le confronte au workflow GitFlow :
// état de chaque branche, branches de travail présentes ou reconstituées
// depuis les messages de fusion, et écarts constatés.
//
// Les lectures indépendantes (lignes permanentes, puis chaque branche) sont
// menées en parallèle : chacune lance un processus git, dont le coût
// domine largement celui de l'analyse elle-même.
func Analyze(h History, opts Options) (Report, error) {
	branches, err := h.Branches()
	if err != nil {
		return Report{}, err
	}
	names := make([]string, len(branches))
	byName := make(map[string]Branch, len(branches))
	for i, b := range branches {
		names[i] = b.Name
		byName[b.Name] = b
	}

	cfg := DefaultConfig()
	if values, err := h.GitflowConfig(); err == nil {
		cfg = ConfigFromGit(values)
	}
	classifier := NewClassifier(names, cfg)
	a := &analysis{h: h, classifier: classifier, byName: byName, opts: opts}

	report := Report{Classifier: classifier}
	_, hasMain := byName[classifier.Main]
	_, hasDevelop := byName[classifier.Develop]

	// Lignes permanentes, fusions qu'elles ont reçues, tags : lectures
	// globales, indépendantes les unes des autres.
	var mainLog, developLog, mainMerges, developMerges []Commit
	var tags map[string][]string
	var developAhead, developBehind int
	var tasks []func()
	if hasMain {
		tasks = append(tasks,
			func() { mainLog, _ = h.FirstParentLog(classifier.Main) },
			func() { mainMerges, _ = h.MergeLog(classifier.Main) },
		)
	}
	if hasDevelop {
		tasks = append(tasks,
			func() { developLog, _ = h.FirstParentLog(classifier.Develop) },
			func() { developMerges, _ = h.MergeLog(classifier.Develop) },
		)
	}
	if hasMain && hasDevelop {
		tasks = append(tasks,
			func() { developAhead, _ = h.CountNotIn(classifier.Develop, classifier.Main) },
			func() { developBehind, _ = h.CountNotIn(classifier.Main, classifier.Develop) },
		)
	}
	tasks = append(tasks, func() { tags, _ = h.Tags() })
	parallel(len(tasks), func(i int) { tasks[i]() })

	a.spines = make(map[string]map[string]bool, 2)
	a.merges = make(mergeIndex, 2)
	if hasMain {
		report.Main = &Spine{Branch: byName[classifier.Main], Log: mainLog, Direct: directCommits(mainLog)}
		a.spines[classifier.Main] = hashSet(mainLog)
		a.merges[classifier.Main] = a.ephemeralMerges(classifier.Main, mainMerges)
	}
	if hasDevelop {
		report.Develop = &Spine{Branch: byName[classifier.Develop], Log: developLog, Direct: directCommits(developLog)}
		a.spines[classifier.Develop] = hashSet(developLog)
		a.merges[classifier.Develop] = a.ephemeralMerges(classifier.Develop, developMerges)
	}
	report.Unreleased = developAhead

	// Branches existantes : état de chacune, et ligne de l'historique pour
	// les branches de travail.
	nodes := classifier.Classify(names)
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	states := make([]BranchState, len(nodes))
	lanes := make([]*Lane, len(nodes))
	parallel(len(nodes), func(i int) {
		n := nodes[i]
		st := BranchState{Node: n, Branch: byName[n.Name]}
		switch n.Type {
		case TypeMain:
			st.Commits = mainLog
		case TypeDevelop:
			st.Commits = developLog
			st.Ahead, st.Behind = developAhead, developBehind
		default:
			if _, ok := byName[n.Parent]; ok {
				st.Ahead, st.Behind, _ = h.AheadBehind(n.Parent, n.Name)
			}
			chain, _ := h.FirstParentLog(n.Name)
			st.Commits = a.ownCommits(n, chain)
			if n.Type.IsEphemeral() {
				lane := a.liveLane(n, chain, st.Commits)
				st.Status, st.StaleDays = lane.Status(), lane.StaleDays
				lanes[i] = &lane
			}
		}
		states[i] = st
	})
	report.Branches = states
	for _, st := range states {
		if st.Branch.IsHead {
			report.CurrentBranch = st.Node.Name
		}
	}

	// Branches supprimées, reconstituées depuis les fusions reçues par les
	// branches permanentes, et synchronisations develop → main.
	var mentioned []string
	seen := make(map[string]bool)
	for _, bySource := range a.merges {
		for name := range bySource {
			if _, exists := byName[name]; !exists && !seen[name] {
				seen[name] = true
				mentioned = append(mentioned, name)
			}
		}
	}
	sort.Strings(mentioned)
	var syncs []MergeRef
	if hasMain && hasDevelop {
		syncs = syncMerges(classifier.Main, classifier.Develop, a.spines[classifier.Main], mainMerges)
	}
	history := make([]*Lane, len(mentioned)+len(syncs))
	parallel(len(history), func(i int) {
		if i < len(mentioned) {
			if lane, ok := a.deletedLane(classifier.ClassifyOne(mentioned[i])); ok {
				history[i] = &lane
			}
			return
		}
		history[i] = a.syncLane(syncs[i-len(mentioned)])
	})

	for _, l := range append(lanes, history...) {
		if l != nil {
			report.Lanes = append(report.Lanes, *l)
		}
	}
	if hasMain {
		checkVersionTags(classifier, classifier.Main, tags, report.Lanes)
	}
	sort.SliceStable(report.Lanes, func(i, j int) bool {
		li, lj := report.Lanes[i], report.Lanes[j]
		if li.Date != lj.Date {
			return li.Date < lj.Date
		}
		return li.Node.Name < lj.Node.Name
	})
	return report, nil
}

// analysis porte le contexte partagé par les étapes de l'analyse.
type analysis struct {
	h          History
	classifier Classifier
	byName     map[string]Branch
	opts       Options
	// spines donne, pour chaque branche permanente, les hashes de sa ligne
	// directe.
	spines map[string]map[string]bool
	merges mergeIndex
}

// parallel appelle fn(0) … fn(n-1) sur un nombre borné de goroutines et
// attend qu'elles aient toutes terminé.
func parallel(n int, fn func(i int)) {
	workers := min(runtime.NumCPU(), 8, n)
	next := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				fn(i)
			}
		}()
	}
	for i := 0; i < n; i++ {
		next <- i
	}
	close(next)
	wg.Wait()
}

func hashSet(commits []Commit) map[string]bool {
	set := make(map[string]bool, len(commits))
	for _, c := range commits {
		set[c.Hash] = true
	}
	return set
}

// directCommits renvoie les commits arrivés directement sur une branche
// permanente, hors fusion, d'après sa ligne directe log (du plus récent au
// plus ancien) : les commits ordinaires postérieurs à sa première fusion,
// c'est-à-dire ceux qui ont contourné le flow une fois celui-ci amorcé. Le
// commit racine et les commits antérieurs à toute fusion (amorçage de la
// branche) sont ignorés, faute d'autre façon de les faire exister, tout
// comme les intégrations de pull request en squash (cf. IsSquashMerge).
func directCommits(log []Commit) []Commit {
	var out []Commit
	seenMerge := false
	for i := len(log) - 1; i >= 0; i-- {
		c := log[i]
		switch {
		case c.IsMerge():
			seenMerge = true
		case len(c.Parents) == 0 || !seenMerge || IsSquashMerge(c.Subject):
		default:
			out = append(out, c)
		}
	}
	// Du plus récent au plus ancien, comme les autres listes de commits.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// mergeIndex recense, pour chaque branche permanente (cible), les fusions de
// branches éphémères qu'elle a reçues : cible → branche source → fusions,
// de la plus récente à la plus ancienne. Une branche peut avoir été
// fusionnée plusieurs fois (travail repris après une première fusion).
type mergeIndex map[string]map[string][]MergeRef

// latest renvoie la fusion la plus récente de name trouvée dans target.
func (idx mergeIndex) latest(target, name string) (MergeRef, bool) {
	events := idx[target][name]
	if len(events) == 0 {
		return MergeRef{}, false
	}
	return events[0], true
}

// unexpected renvoie, pour chaque cible non prévue par GitFlow pour node, la
// plus récente des fusions de node qu'elle a reçues, sauf si c'est le même
// commit qu'une fusion vers une cible valide : cas d'une branche principale
// avancée en fast-forward sur develop, dont la ligne directe reprend alors
// celle de develop.
func (idx mergeIndex) unexpected(node Node) []MergeRef {
	valid := make(map[string]bool, len(node.MergeTargets))
	expected := make(map[string]bool)
	for _, target := range node.MergeTargets {
		valid[target] = true
		for _, ev := range idx[target][node.Name] {
			expected[ev.Hash] = true
		}
	}
	var out []MergeRef
	for target, bySource := range idx {
		if valid[target] {
			continue
		}
		for _, ev := range bySource[node.Name] {
			if !expected[ev.Hash] {
				out = append(out, ev)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

// mergeRefOf décrit le commit de fusion c reçu par target.
func mergeRefOf(target string, c Commit) MergeRef {
	mr := MergeRef{Target: target, Date: c.CommitDate, Hash: c.Hash}
	if len(c.Parents) > 1 {
		mr.Base, mr.Tip = c.Parents[0], c.Parents[1]
	}
	return mr
}

// ephemeralMerges groupe par branche source les fusions de branches
// feature/bugfix/release/hotfix reçues par target, parmi merges, les
// commits de fusion atteignables depuis target. Seules comptent celles de
// sa ligne directe : les autres ont été faites ailleurs (dans develop, dans
// une release…) et ne sont arrivées dans target qu'avec la branche qui les
// contenait — la fusion d'une release dans develop, atteignable depuis main
// après la release suivante, n'est pas une fusion dans main. Un git pull
// (fusion d'une branche dans sa propre copie) n'en est pas une non plus.
func (a *analysis) ephemeralMerges(target string, merges []Commit) map[string][]MergeRef {
	events := make(map[string][]MergeRef)
	spine := a.spines[target]
	for _, c := range merges {
		if !spine[c.Hash] {
			continue
		}
		m, ok := ParseMerge(c.Subject)
		if !ok || m.IsPull() || !a.classifier.ClassifyOne(m.Source).Type.IsEphemeral() {
			continue
		}
		events[m.Source] = append(events[m.Source], mergeRefOf(target, c))
	}
	return events
}

// syncMerges retrouve, parmi les commits de fusion atteignables depuis
// main, chaque fusion de develop dans main (message git standard, ou merge
// de pull request), c'est-à-dire sur la ligne directe de main (mainSpine).
// Les fusions de develop vers une autre branche (ex. une feature mise à
// jour depuis develop), également atteignables depuis main, sont écartées.
func syncMerges(mainName, developName string, mainSpine map[string]bool, mainMerges []Commit) []MergeRef {
	var out []MergeRef
	for _, c := range mainMerges {
		if !mainSpine[c.Hash] {
			continue
		}
		m, ok := ParseMerge(c.Subject)
		if ok && !m.IsPull() && m.Source == developName && (m.Target == "" || m.Target == mainName) {
			out = append(out, mergeRefOf(mainName, c))
		}
	}
	return out
}

// nearestMatch renvoie la position, dans chain (une ligne directe classée
// du plus récent au plus ancien), du premier commit appartenant à l'un des
// ensembles sets — ou -1 si aucun ne correspond.
func nearestMatch(chain []Commit, sets ...map[string]bool) int {
	for i, c := range chain {
		for _, set := range sets {
			if set[c.Hash] {
				return i
			}
		}
	}
	return -1
}

// ownCommits renvoie le travail propre de la branche n, d'après sa ligne
// directe chain : ses commits jusqu'à son point de divergence avec sa
// branche parente (ou, pour une branche hors GitFlow, avec main ou
// develop). Contrairement à une simple différence avec la parente, cela
// reste vrai une fois la branche fusionnée, quand sa parente contient tous
// ses commits.
func (a *analysis) ownCommits(n Node, chain []Commit) []Commit {
	var sets []map[string]bool
	if n.Parent != "" {
		sets = append(sets, a.spines[n.Parent])
	} else {
		for _, set := range a.spines {
			sets = append(sets, set)
		}
	}
	idx := nearestMatch(chain, sets...)
	if idx <= 0 {
		// Pas de parente connue, ou pointe déjà sur une ligne permanente :
		// branche tout juste créée, sans commit propre (ou intégrée en
		// fast-forward, indiscernable).
		return nil
	}
	return chain[:idx]
}

// divergence trouve le vrai point de divergence de n : le premier commit de
// sa ligne directe chain (en partant de la pointe) qui appartient aussi à
// l'une des deux lignes permanentes — pas un simple ancêtre commun lointain
// (une fois la branche fusionnée partout, un merge-base avec n'importe
// quelle branche renvoie trivialement sa propre pointe). Si ce point est
// plus proche de la pointe côté de l'autre branche permanente que côté du
// parent attendu, la branche a probablement été créée depuis cette autre
// branche : wrongParent en donne alors le nom.
func (a *analysis) divergence(n Node, chain []Commit) (fork Commit, found bool, wrongParent string) {
	other := a.classifier.Main
	if n.Parent == a.classifier.Main {
		other = a.classifier.Develop
	}
	idxExpected := nearestMatch(chain, a.spines[n.Parent])
	idxOther := nearestMatch(chain, a.spines[other])
	switch {
	case idxOther >= 0 && (idxExpected < 0 || idxOther < idxExpected):
		return chain[idxOther], true, other
	case idxExpected >= 0:
		return chain[idxExpected], true, ""
	}
	return Commit{}, false, ""
}

// laneDate renvoie la position d'une ligne sur l'axe du temps : la date de
// sa première intégration connue (fusion vers une cible valide ou non),
// sinon fallback — le point de divergence d'une branche pas encore
// fusionnée. Toutes les dates sont au même format (ISO, fuseau local) et se
// comparent donc comme des chaînes.
func laneDate(merged, unexpected []MergeRef, fallback string) string {
	date := ""
	for _, refs := range [][]MergeRef{merged, unexpected} {
		for _, mr := range refs {
			if mr.Date != "" && (date == "" || mr.Date < date) {
				date = mr.Date
			}
		}
	}
	if date == "" {
		return fallback
	}
	return date
}

// liveLane construit la ligne d'une branche éphémère existante : fusions
// vers chacune de ses cibles (identifiées par leur commit de fusion, ou à
// défaut par ascendance), cibles en attente, fusions hors flow, point de
// divergence et inactivité.
func (a *analysis) liveLane(n Node, chain, commits []Commit) Lane {
	branch := a.byName[n.Name]
	lane := Lane{Node: n, Branch: branch, Kind: LaneLive, Commits: commits}
	for _, target := range n.MergeTargets {
		if ev, ok := a.merges.latest(target, n.Name); ok {
			lane.Merged = append(lane.Merged, ev)
			continue
		}
		// Une branche sans commit propre (tout juste créée) est trivialement
		// contenue dans sa parente : ce n'est pas pour autant une fusion.
		if len(commits) > 0 {
			if merged, err := a.h.IsAncestor(n.Name, target); err == nil && merged {
				lane.Merged = append(lane.Merged, MergeRef{Target: target})
				continue
			}
		}
		lane.Pending = append(lane.Pending, target)
	}
	lane.Unexpected = a.merges.unexpected(n)

	fork, found, wrongParent := a.divergence(n, chain)
	lane.WrongParent = wrongParent
	fallback := branch.CommitDate
	if found {
		fallback = fork.CommitDate
	}
	lane.Date = laneDate(lane.Merged, lane.Unexpected, fallback)
	lane.StaleDays = staleDays(lane, a.opts.StaleAfter, a.opts.Now)
	return lane
}

// staleDays renvoie le nombre de jours écoulés depuis le dernier commit
// d'une branche pas encore complètement fusionnée, s'il dépasse
// staleAfter ; 0 sinon (seuil nul, branche fusionnée, date illisible).
func staleDays(l Lane, staleAfter time.Duration, now time.Time) int {
	if staleAfter <= 0 || len(l.Pending) == 0 {
		return 0
	}
	last, err := time.Parse("2006-01-02 15:04:05 -0700", l.Branch.CommitDate)
	if err != nil {
		return 0
	}
	idle := now.Sub(last)
	if idle < staleAfter {
		return 0
	}
	return max(int(idle.Hours()/24), 1)
}

// deletedLane reconstitue, depuis les fusions reçues par les branches
// permanentes, la ligne d'une branche éphémère supprimée. ok vaut false si
// aucune fusion de la branche n'a été trouvée.
func (a *analysis) deletedLane(node Node) (Lane, bool) {
	// On ne retient comme fusion "normale" que les cibles réellement
	// valides pour ce type de branche (ex. develop pour une feature).
	var refs []MergeRef
	for _, target := range node.MergeTargets {
		if ev, ok := a.merges.latest(target, node.Name); ok {
			refs = append(refs, ev)
		}
	}

	// Une fusion trouvée dans une cible non valide (ex. main pour une
	// feature) est soit un écho transitif (même commit que l'une des fusions
	// valides — main a ensuite intégré develop), soit une vraie anomalie :
	// une fusion directe hors du flow attendu.
	unexpected := a.merges.unexpected(node)

	if len(refs) == 0 && len(unexpected) == 0 {
		return Lane{}, false
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Date < refs[j].Date })

	// Première fusion réellement identifiée : elle a apporté les commits de
	// la branche, et son second parent est la pointe de la branche.
	var known MergeRef
	if len(refs) > 0 {
		known = refs[0]
	} else {
		known = unexpected[0]
	}

	// Une cible valide sans fusion identifiée peut malgré tout contenir le
	// travail de la branche, arrivé par un autre chemin (hotfix fusionné
	// dans la release en cours, qui a ensuite rejoint develop) : il suffit
	// de vérifier que la cible contient la pointe de la branche. Sinon la
	// cible n'a jamais été atteinte.
	var pending []string
	for _, target := range node.MergeTargets {
		if _, ok := a.merges.latest(target, node.Name); ok {
			continue
		}
		if known.Tip != "" {
			if merged, err := a.h.IsAncestor(known.Tip, target); err == nil && merged {
				refs = append(refs, MergeRef{Target: target})
				continue
			}
		}
		pending = append(pending, target)
	}

	lane := Lane{
		Node:       node,
		Kind:       LaneDeleted,
		Merged:     refs,
		Unexpected: unexpected,
		Pending:    pending,
		Date:       laneDate(refs, unexpected, ""),
	}
	if known.Tip != "" {
		lane.Commits, _ = a.h.CommitsBetween(known.Base, known.Tip)
	}
	return lane, true
}

// syncLane construit la ligne d'une fusion directe de develop dans main.
func (a *analysis) syncLane(ev MergeRef) *Lane {
	node := Node{
		Name:   a.classifier.Develop + " → " + a.classifier.Main,
		Type:   TypeOther,
		Parent: a.classifier.Develop,
	}
	lane := &Lane{Node: node, Kind: LaneSync, Merged: []MergeRef{ev}, Date: ev.Date}
	if ev.Tip != "" {
		lane.Commits, _ = a.h.CommitsBetween(ev.Base, ev.Tip)
	}
	return lane
}

// checkVersionTags retrouve le tag de version de chaque fusion de release
// ou de hotfix dans mainName : posé sur le commit de fusion (git flow), ou
// à défaut sur la pointe de la branche fusionnée. Une fusion sans tag est
// signalée (UntaggedIn) — sauf si le dépôt n'a aucun tag de version, signe
// qu'il ne suit pas cette convention.
func checkVersionTags(classifier Classifier, mainName string, all map[string][]string, lanes []Lane) {
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
		if l.Node.Type != TypeRelease && l.Node.Type != TypeHotfix {
			continue
		}
		for j := range l.Merged {
			mr := &l.Merged[j]
			if mr.Target != mainName || mr.Hash == "" {
				continue
			}
			found := tags[mr.Hash]
			if len(found) == 0 && mr.Tip != "" {
				found = tags[mr.Tip]
			}
			if len(found) > 0 {
				sort.Strings(found)
				mr.Tag = found[0]
			} else {
				l.UntaggedIn = mainName
			}
		}
	}
}

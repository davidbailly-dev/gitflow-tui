// Package tui implémente l'interface Bubble Tea de gitflow-tui : une vue en
// colonnes par type de branche GitFlow, et une vue graphe de commits, avec
// bascule et navigation entre les deux.
package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"gitflow-tui/internal/git"
	"gitflow-tui/internal/gitflow"
)

// pollInterval est la fréquence de vérification d'un changement dans le
// dépôt (nouveau commit, fusion, branche créée/supprimée) pour le
// rafraîchissement automatique. Chaque tick ne fait que comparer une
// empreinte bon marché (lecture de fichiers, sans lancer git) ; le
// rechargement complet n'est déclenché que si elle a changé.
const pollInterval = 2 * time.Second

// pollTickMsg déclenche une vérification de l'empreinte du dépôt.
type pollTickMsg struct{}

func pollTick() tea.Cmd {
	return tea.Tick(pollInterval, func(time.Time) tea.Msg { return pollTickMsg{} })
}

type view int

const (
	viewColumns view = iota
	viewGraph
)

// pane sélectionne, dans la vue colonnes, lequel des trois panneaux reçoit
// la navigation (↑/↓, ou le défilement pour paneDiff) : les branches, les
// commits/fusions de la branche sélectionnée, ou le contenu du commit
// sélectionné.
type pane int

const (
	paneBranches pane = iota
	paneCommits
	paneDiff
)

// graphMode sélectionne ce que montre la vue graphe : l'historique complet
// (branches supprimées reconstituées, synchronisations minées, alertes
// détectées en scannant tout le log), ou seulement l'état courant du dépôt
// (branches locales encore en attente d'une fusion).
type graphMode int

const (
	graphHistory graphMode = iota
	graphLive
)

// item est une branche prête à être affichée : sa classification GitFlow,
// ses données git, et son statut par rapport à sa branche parente.
type item struct {
	node    gitflow.Node
	branch  git.Branch
	ahead   int
	behind  int
	commits []git.Commit
}

type column struct {
	title string
	items []item
}

// branchRow est une ligne du panneau de gauche de la vue colonnes : soit un
// en-tête de groupe (non sélectionnable), soit un indicateur "(vide)" pour
// un groupe sans branche, soit une branche.
type branchRow struct {
	header string
	empty  bool
	it     item
}

// dataLoadedMsg porte le résultat d'un chargement (initial, manuel via 'r',
// ou automatique en arrière-plan). fingerprint est l'empreinte du dépôt
// telle qu'elle était juste avant les lectures git : la comparer à
// l'empreinte courante lors du tick suivant permet de rattraper tout
// changement survenu pendant le chargement lui-même.
type dataLoadedMsg struct {
	columns     []column
	timeline    timeline
	fingerprint string
	err         error
}

// diffLoadedMsg porte le contenu (git show) du commit demandé. hash permet
// d'ignorer un résultat devenu obsolète si la sélection a changé entre
// temps.
type diffLoadedMsg struct {
	hash    string
	content string
	err     error
}

// Model est le modèle racine Bubble Tea de gitflow-tui.
type Model struct {
	repo git.Repository

	width, height int

	view      view
	graphMode graphMode
	// historyAlertsOnly restreint la vue graphe en mode historique aux
	// seules lignes signalant une déviation du workflow GitFlow standard.
	// Actif par défaut : l'historique complet est bruyant sur un dépôt
	// mature, alors que ce qui mérite l'attention est justement ce qui en
	// dévie.
	historyAlertsOnly bool
	columns           []column
	paneFocus         pane
	branchIdx         int
	commitIdx         int
	// selectedBranchName et selectedCommitHash retiennent la sélection
	// actuelle par identité plutôt que par index, pour la retrouver après un
	// rechargement des données même si l'ajout ou la suppression d'une
	// branche ou d'un commit a décalé les positions.
	selectedBranchName string
	selectedCommitHash string

	filter    string
	filtering bool

	timeline timeline
	graph    viewport.Model

	diff    viewport.Model
	diffFor string

	showHelp bool
	loading  bool
	// refreshing indique qu'un chargement (manuel ou automatique) est déjà
	// en cours, pour éviter de superposer deux appels git concurrents.
	refreshing bool
	// lastFingerprint est l'empreinte du dépôt correspondant aux données
	// actuellement affichées ; comparée à chaque tick pour détecter un
	// changement à rafraîchir automatiquement.
	lastFingerprint string
	err             error
}

// New construit le modèle initial pour le dépôt donné.
func New(repo git.Repository) Model {
	return Model{
		repo:              repo,
		view:              viewColumns,
		graph:             viewport.New(0, 0),
		diff:              viewport.New(0, 0),
		loading:           true,
		historyAlertsOnly: true,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(loadData(m.repo), pollTick())
}

func loadData(repo git.Repository) tea.Cmd {
	return func() tea.Msg {
		fingerprint := repo.StateFingerprint()

		branches, err := repo.Branches()
		if err != nil {
			return dataLoadedMsg{fingerprint: fingerprint, err: err}
		}

		names := make([]string, len(branches))
		byName := make(map[string]git.Branch, len(branches))
		for i, b := range branches {
			names[i] = b.Name
			byName[b.Name] = b
		}

		classifier := gitflow.NewClassifier(names)
		nodes := classifier.Classify(names)

		var permanent []string
		for _, name := range []string{classifier.Main, classifier.Develop} {
			if _, ok := byName[name]; ok {
				permanent = append(permanent, name)
			}
		}
		sp := loadSpines(repo, permanent...)

		// main et develop listent tout leur historique direct ; les autres
		// branches, leurs seuls commits propres (partagés avec le graphe).
		commitsByName := make(map[string][]git.Commit, len(nodes))
		toItem := func(n gitflow.Node) item {
			it := item{node: n, branch: byName[n.Name]}
			if n.Parent != "" {
				if ahead, behind, err := repo.AheadBehind(n.Parent, n.Name); err == nil {
					it.ahead, it.behind = ahead, behind
				}
			}
			switch n.Type {
			case gitflow.TypeMain, gitflow.TypeDevelop:
				if commits, err := repo.FirstParentCommits(n.Name); err == nil {
					it.commits = commits
				}
			default:
				it.commits = ownCommits(repo, n, sp)
			}
			commitsByName[n.Name] = it.commits
			return it
		}

		var mains, develops, features, releases, hotfixes, others []gitflow.Node

		for _, n := range nodes {
			switch n.Type {
			case gitflow.TypeMain:
				mains = append(mains, n)
			case gitflow.TypeDevelop:
				develops = append(develops, n)
			case gitflow.TypeFeature:
				features = append(features, n)
			case gitflow.TypeRelease:
				releases = append(releases, n)
			case gitflow.TypeHotfix:
				hotfixes = append(hotfixes, n)
			default:
				others = append(others, n)
			}
		}

		byName2 := func(ns []gitflow.Node) func(i, j int) bool {
			return func(i, j int) bool { return ns[i].Name < ns[j].Name }
		}
		sort.Slice(mains, byName2(mains))
		sort.Slice(develops, byName2(develops))
		sort.Slice(features, byName2(features))
		sort.Slice(releases, byName2(releases))
		sort.Slice(hotfixes, byName2(hotfixes))
		sort.Slice(others, byName2(others))

		build := func(ns []gitflow.Node) []item {
			its := make([]item, 0, len(ns))
			for _, n := range ns {
				its = append(its, toItem(n))
			}
			return its
		}

		cols := []column{
			{title: "main", items: build(mains)},
			{title: "develop", items: build(develops)},
			{title: "feature/*", items: build(features)},
			{title: "release/*", items: build(releases)},
			{title: "hotfix/*", items: build(hotfixes)},
			{title: "autre", items: build(others)},
		}

		tl := buildTimeline(repo, classifier, nodes, byName, sp, commitsByName)

		return dataLoadedMsg{columns: cols, timeline: tl, fingerprint: fingerprint}
	}
}

// currentGraphContent rend la vue graphe selon le mode actif, sans refaire
// d'appel git : historique complet, ou seulement l'état courant du dépôt.
func (m Model) currentGraphContent() string {
	tl := m.timeline
	emptyMsg := "Aucune branche feature/release/hotfix, active ou fusionnée."
	switch {
	case m.graphMode == graphLive:
		tl = filterLive(tl)
		emptyMsg = "Aucune branche en attente de fusion pour le moment."
	case m.historyAlertsOnly:
		tl = filterAlerts(tl)
		emptyMsg = "Historique propre : aucune déviation du workflow GitFlow détectée."
	}
	return renderTimeline(tl, emptyMsg)
}

// loadDiff récupère en tâche de fond le contenu complet (git show) du commit
// c. Si c'est une fusion, la liste des commits qu'elle a apportés (souvent
// invisibles dans l'historique premier-parent de main/develop) est ajoutée
// avant le diff, puisque celui-ci est en pratique peu informatif pour une
// fusion propre (git montre alors un diff combiné, généralement vide).
func loadDiff(repo git.Repository, c git.Commit, width int) tea.Cmd {
	return func() tea.Msg {
		var b strings.Builder
		if c.IsMerge {
			if commits, err := repo.MergeCommits(c.Hash); err == nil && len(commits) > 0 {
				b.WriteString(renderMergedCommits(commits, width))
				b.WriteString("\n")
			}
		}
		content, err := repo.Show(c.Hash)
		if err != nil {
			return diffLoadedMsg{hash: c.Hash, err: err}
		}
		b.WriteString(content)
		return diffLoadedMsg{hash: c.Hash, content: b.String()}
	}
}

// renderMergedCommits liste les commits apportés par une fusion. Chaque
// ligne est tronquée à width plutôt que laissée au retour à la ligne
// automatique du viewport : ce dernier ne réapplique pas toujours la
// couleur sur la partie repliée, qui apparaît alors en blanc.
func renderMergedCommits(commits []git.Commit, width int) string {
	if width < 1 {
		width = 1
	}
	headerStyle := lipgloss.NewStyle().Foreground(colorWarning)
	commitStyle := lipgloss.NewStyle().Foreground(colorFeature)

	// Format volontairement court (pas de date) pour limiter le retour à la
	// ligne dans le panneau, plus étroit qu'un terminal plein écran.
	var b strings.Builder
	header := ansi.Truncate(fmt.Sprintf("⑂ %d commit(s) fusionné(s) :", len(commits)), width, "…")
	fmt.Fprintf(&b, "%s\n", headerStyle.Render(header))
	for _, mc := range commits {
		line := ansi.Truncate(fmt.Sprintf("· %s %s", mc.Hash, mc.Subject), width, "…")
		fmt.Fprintf(&b, "%s\n", commitStyle.Render(line))
	}
	return b.String()
}

// paneContentHeight renvoie la hauteur de contenu disponible pour chacun
// des trois panneaux de la vue colonnes (hauteur totale moins l'en-tête, le
// pied de page, et la bordure du panneau).
func (m Model) paneContentHeight() int {
	h := m.height - 2 - 2
	if h < 3 {
		h = 3
	}
	return h
}

// paneWidths renvoie la largeur externe (bordure comprise) de chacun des
// trois panneaux de la vue colonnes : le panneau des branches (1) est plus
// étroit — il n'affiche que des noms —, les panneaux des commits (2) et du
// contenu (3) se partagent le reste à parts égales, puisqu'ils portent
// l'essentiel de l'information. Les trois doivent toujours tenir dans la
// largeur réelle du terminal : un plancher trop élevé forcerait le terminal
// lui-même à retourner à la ligne, ce qui casse le positionnement du
// curseur de bubbletea et fait disparaître des colonnes entières. Les
// panneaux rétrécissent donc plutôt que de déborder ; le défilement propre
// à chaque panneau prend le relais pour rester lisible.
func (m Model) paneWidths() (branches, commits, diff int) {
	avail := m.width - 6 // 3 panneaux x 2 caractères de bordure chacun
	if avail < 18 {
		avail = 18
	}
	branches = avail / 5
	if branches < 6 {
		branches = 6
	}
	rest := avail - branches
	commits = rest / 2
	diff = rest - commits
	if commits < 6 {
		commits = 6
	}
	if diff < 6 {
		diff = 6
	}
	return
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		graphH := m.height - 3
		if graphH < 0 {
			graphH = 0
		}
		m.graph.Width = m.width
		m.graph.Height = graphH

		// La largeur intérieure du viewport doit correspondre à celle que le
		// panneau lui laissera réellement (largeur externe moins le padding
		// horizontal du cadre) : un écart ferait replier certaines lignes une
		// seconde fois lors du rendu final, ajoutant des lignes imprévues.
		_, _, diffWidth := m.paneWidths()
		m.diff.Width = diffWidth - 2
		m.diff.Height = m.paneContentHeight() - 1
		return m, nil

	case dataLoadedMsg:
		m.loading = false
		m.refreshing = false
		m.err = msg.err
		if msg.err != nil {
			return m, nil
		}
		m.lastFingerprint = msg.fingerprint
		m.columns = msg.columns
		m.timeline = msg.timeline
		m.graph.SetContent(m.currentGraphContent())
		m.restoreBranchSelection()
		m.restoreCommitSelection()
		return m, m.syncDiff()

	case pollTickMsg:
		next := pollTick()
		if m.loading || m.refreshing {
			return m, next
		}
		if m.repo.StateFingerprint() == m.lastFingerprint {
			return m, next
		}
		m.refreshing = true
		return m, tea.Batch(next, loadData(m.repo))

	case diffLoadedMsg:
		if msg.hash != m.diffFor {
			return m, nil
		}
		if msg.err != nil {
			m.diff.SetContent(styleFaint.Render("Erreur : " + msg.err.Error()))
		} else {
			m.diff.SetContent(msg.content)
		}
		// SetContent ne réinitialise pas le défilement : sans ça, la
		// position laissée par un diff précédent (parfois déjà tout en bas)
		// s'appliquerait telle quelle à ce nouveau contenu, plus court.
		m.diff.GotoTop()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyEsc:
			m.filtering = false
			m.filter = ""
		case tea.KeyEnter:
			m.filtering = false
		case tea.KeyBackspace:
			if len(m.filter) > 0 {
				r := []rune(m.filter)
				m.filter = string(r[:len(r)-1])
			}
		case tea.KeyRunes:
			m.filter += string(msg.Runes)
		}
		m.ensureBranchSelection()
		m.commitIdx = 0
		return m, m.syncDiff()
	}

	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Help):
		m.showHelp = !m.showHelp
		return m, nil
	case key.Matches(msg, keys.Refresh):
		// Les données actuelles restent affichées pendant le rechargement
		// (seul l'en-tête le signale), comme pour le rafraîchissement
		// automatique ; inutile d'en lancer un second s'il est déjà en cours.
		if m.refreshing {
			return m, nil
		}
		m.refreshing = true
		return m, loadData(m.repo)
	case key.Matches(msg, keys.Tab):
		if m.view == viewColumns {
			m.view = viewGraph
		} else {
			m.view = viewColumns
		}
		return m, nil
	case key.Matches(msg, keys.Mode):
		if m.view == viewGraph {
			if m.graphMode == graphHistory {
				m.graphMode = graphLive
			} else {
				m.graphMode = graphHistory
			}
			m.graph.SetContent(m.currentGraphContent())
		}
		return m, nil
	case key.Matches(msg, keys.Alerts):
		if m.view == viewGraph && m.graphMode == graphHistory {
			m.historyAlertsOnly = !m.historyAlertsOnly
			m.graph.SetContent(m.currentGraphContent())
		}
		return m, nil
	case key.Matches(msg, keys.Filter):
		if m.view == viewColumns {
			m.filtering = true
		}
		return m, nil
	}

	if m.view == viewGraph {
		var cmd tea.Cmd
		m.graph, cmd = m.graph.Update(msg)
		return m, cmd
	}

	// Vue colonnes : ←/→ change de panneau quel que soit celui actif, y
	// compris depuis le panneau "contenu" — sinon, une fois son viewport
	// focalisé, plus aucune touche n'en ressort puisqu'il les consomme
	// toutes pour son propre défilement.
	switch {
	case key.Matches(msg, keys.Left):
		if m.paneFocus > paneBranches {
			m.paneFocus--
		}
		return m, nil
	case key.Matches(msg, keys.Right):
		if m.paneFocus < paneDiff {
			m.paneFocus++
		}
		return m, nil
	}

	// Le panneau "contenu" est un viewport à part entière (défilement libre
	// du diff), les deux autres sont des listes à sélection unique.
	if m.paneFocus == paneDiff {
		var cmd tea.Cmd
		m.diff, cmd = m.diff.Update(msg)
		return m, cmd
	}

	switch {
	case key.Matches(msg, keys.Up):
		return m, m.moveSelection(-1)
	case key.Matches(msg, keys.Down):
		return m, m.moveSelection(1)
	}
	return m, nil
}

// moveSelection déplace la sélection du panneau actif (branches ou
// commits/fusions) et renvoie, le cas échéant, la commande de chargement du
// nouveau contenu à afficher dans le panneau "contenu".
func (m *Model) moveSelection(d int) tea.Cmd {
	switch m.paneFocus {
	case paneBranches:
		m.moveBranch(d)
		m.commitIdx = 0
		m.syncSelectedCommitHash()
	case paneCommits:
		m.moveCommit(d)
	}
	return m.syncDiff()
}

func (m *Model) moveBranch(d int) {
	rows := m.flatBranches()
	n := len(rows)
	if n == 0 {
		return
	}
	i := m.branchIdx
	for step := 0; step < n; step++ {
		i = ((i+d)%n + n) % n
		if selectableRow(rows, i) {
			m.branchIdx = i
			m.syncSelectedBranchName()
			return
		}
	}
}

func (m *Model) moveCommit(d int) {
	it, ok := m.selectedBranchRow()
	if !ok {
		return
	}
	n := len(it.commits)
	if n == 0 {
		return
	}
	m.commitIdx = ((m.commitIdx+d)%n + n) % n
	m.syncSelectedCommitHash()
}

// syncDiff s'assure que le panneau "contenu" correspond au commit
// actuellement sélectionné, et déclenche son chargement si besoin.
func (m *Model) syncDiff() tea.Cmd {
	c, ok := m.selectedCommit()
	if !ok {
		m.diffFor = ""
		m.diff.SetContent(styleFaint.Render("Aucun commit sélectionné."))
		m.diff.GotoTop()
		return nil
	}
	if c.Hash == m.diffFor {
		return nil
	}
	m.diffFor = c.Hash
	m.diff.SetContent(styleFaint.Render("Chargement..."))
	m.diff.GotoTop()
	return loadDiff(m.repo, c, m.diff.Width)
}

func selectableRow(rows []branchRow, i int) bool {
	return i >= 0 && i < len(rows) && rows[i].header == "" && !rows[i].empty
}

// ensureBranchSelection fait sauter la sélection vers la première branche
// visible si la ligne active n'en est plus une, typiquement après une
// modification du filtre. Tient à jour selectedBranchName au passage.
func (m *Model) ensureBranchSelection() {
	rows := m.flatBranches()
	if selectableRow(rows, m.branchIdx) {
		m.syncSelectedBranchName()
		return
	}
	for i := range rows {
		if selectableRow(rows, i) {
			m.branchIdx = i
			m.syncSelectedBranchName()
			return
		}
	}
	m.branchIdx = 0
	m.selectedBranchName = ""
}

// restoreBranchSelection retrouve, après un rechargement des données, la
// ligne correspondant à la branche qui était sélectionnée avant coup — par
// son nom, pas son index, puisqu'une branche ajoutée ou supprimée décale
// les positions sans changer les noms des autres. Au premier chargement,
// aucune branche n'est encore retenue : la sélection part de la branche
// courante (HEAD). Si la branche retenue n'existe plus (supprimée
// entre-temps), retombe sur la première branche visible.
func (m *Model) restoreBranchSelection() {
	rows := m.flatBranches()
	if m.selectedBranchName == "" {
		for i, row := range rows {
			if selectableRow(rows, i) && row.it.branch.IsHead {
				m.branchIdx = i
				m.syncSelectedBranchName()
				return
			}
		}
	}
	if m.selectedBranchName != "" {
		for i, row := range rows {
			if selectableRow(rows, i) && row.it.node.Name == m.selectedBranchName {
				m.branchIdx = i
				return
			}
		}
	}
	m.ensureBranchSelection()
}

// syncSelectedBranchName met à jour le nom de la branche sélectionnée
// d'après l'index courant, pour pouvoir la retrouver après un futur
// rechargement des données.
func (m *Model) syncSelectedBranchName() {
	rows := m.flatBranches()
	if selectableRow(rows, m.branchIdx) {
		m.selectedBranchName = rows[m.branchIdx].it.node.Name
	}
}

// restoreCommitSelection retrouve, après un rechargement des données, le
// commit qui était sélectionné avant coup — par son hash — pour que le
// panneau de contenu ne change pas silencieusement de commit à cause d'un
// nouveau commit apparu en tête de liste. Si ce commit n'existe plus dans
// la liste rechargée, retombe sur le premier commit.
func (m *Model) restoreCommitSelection() {
	it, ok := m.selectedBranchRow()
	if !ok {
		m.commitIdx = 0
		m.selectedCommitHash = ""
		return
	}
	if m.selectedCommitHash != "" {
		for i, c := range it.commits {
			if c.Hash == m.selectedCommitHash {
				m.commitIdx = i
				return
			}
		}
	}
	m.commitIdx = 0
	m.syncSelectedCommitHash()
}

// syncSelectedCommitHash met à jour le hash du commit sélectionné d'après
// l'index courant, pour pouvoir le retrouver après un futur rechargement
// des données.
func (m *Model) syncSelectedCommitHash() {
	if c, ok := m.selectedCommit(); ok {
		m.selectedCommitHash = c.Hash
	} else {
		m.selectedCommitHash = ""
	}
}

func (m Model) visibleItems(col column) []item {
	if m.filter == "" {
		return col.items
	}
	needle := strings.ToLower(m.filter)
	out := make([]item, 0, len(col.items))
	for _, it := range col.items {
		if strings.Contains(strings.ToLower(it.node.Name), needle) {
			out = append(out, it)
		}
	}
	return out
}

// flatBranches aplatit les colonnes groupées par type GitFlow en une liste
// unique de lignes pour le panneau de gauche de la vue colonnes : un en-tête
// par groupe, un indicateur "(vide)" si le groupe filtré n'a plus de
// résultat, puis ses branches.
func (m Model) flatBranches() []branchRow {
	var rows []branchRow
	for _, col := range m.columns {
		rows = append(rows, branchRow{header: col.title})
		items := m.visibleItems(col)
		if len(items) == 0 {
			rows = append(rows, branchRow{empty: true})
		}
		for _, it := range items {
			rows = append(rows, branchRow{it: it})
		}
	}
	return rows
}

func (m Model) selectedBranchRow() (item, bool) {
	rows := m.flatBranches()
	if !selectableRow(rows, m.branchIdx) {
		return item{}, false
	}
	return rows[m.branchIdx].it, true
}

func (m Model) selectedCommit() (git.Commit, bool) {
	it, ok := m.selectedBranchRow()
	if !ok || len(it.commits) == 0 {
		return git.Commit{}, false
	}
	idx := m.commitIdx
	if idx < 0 || idx >= len(it.commits) {
		idx = 0
	}
	return it.commits[idx], true
}

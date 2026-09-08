// Package tui implémente l'interface Bubble Tea de gitflow-tui : une vue en
// colonnes par type de branche GitFlow, et une vue graphe de commits, avec
// bascule et navigation entre les deux.
package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"gitflow-tui/internal/git"
	"gitflow-tui/internal/gitflow"
)

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

type dataLoadedMsg struct {
	columns  []column
	timeline timeline
	err      error
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
	columns   []column
	paneFocus pane
	branchIdx int
	commitIdx int

	filter    string
	filtering bool

	timeline timeline
	graph    viewport.Model

	diff    viewport.Model
	diffFor string

	showHelp bool
	loading  bool
	err      error
}

// New construit le modèle initial pour le dépôt donné.
func New(repo git.Repository) Model {
	return Model{
		repo:    repo,
		view:    viewColumns,
		graph:   viewport.New(0, 0),
		diff:    viewport.New(0, 0),
		loading: true,
	}
}

func (m Model) Init() tea.Cmd {
	return loadData(m.repo)
}

func loadData(repo git.Repository) tea.Cmd {
	return func() tea.Msg {
		branches, err := repo.Branches()
		if err != nil {
			return dataLoadedMsg{err: err}
		}

		names := make([]string, len(branches))
		byName := make(map[string]git.Branch, len(branches))
		for i, b := range branches {
			names[i] = b.Name
			byName[b.Name] = b
		}

		nodes := gitflow.Classify(names)

		toItem := func(n gitflow.Node) item {
			it := item{node: n, branch: byName[n.Name]}
			if n.Parent != "" {
				if ahead, behind, err := repo.AheadBehind(n.Parent, n.Name); err == nil {
					it.ahead, it.behind = ahead, behind
				}
				if commits, err := repo.CommitsNotIn(n.Name, n.Parent); err == nil {
					it.commits = commits
				}
			} else if commits, err := repo.FirstParentCommits(n.Name); err == nil {
				it.commits = commits
			}
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

		tl := buildTimeline(repo, nodes, byName)

		return dataLoadedMsg{columns: cols, timeline: tl}
	}
}

// currentGraphContent rend la vue graphe selon le mode actif, sans refaire
// d'appel git : historique complet, ou seulement l'état courant du dépôt.
func (m Model) currentGraphContent() string {
	tl := m.timeline
	emptyMsg := "Aucune branche feature/release/hotfix, active ou fusionnée."
	if m.graphMode == graphLive {
		tl = filterLive(tl)
		emptyMsg = "Aucune branche en attente de fusion pour le moment."
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
		m.err = msg.err
		if msg.err != nil {
			return m, nil
		}
		m.columns = msg.columns
		m.timeline = msg.timeline
		m.graph.SetContent(m.currentGraphContent())
		m.ensureBranchSelection()
		return m, m.syncDiff()

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
		m.loading = true
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
// modification du filtre ou un rafraîchissement des données.
func (m *Model) ensureBranchSelection() {
	rows := m.flatBranches()
	if selectableRow(rows, m.branchIdx) {
		return
	}
	for i := range rows {
		if selectableRow(rows, i) {
			m.branchIdx = i
			return
		}
	}
	m.branchIdx = 0
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

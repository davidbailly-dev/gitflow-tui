// Package tui implémente l'interface Bubble Tea de gitflow-tui : une vue en
// colonnes par type de branche GitFlow, et une vue graphe de commits, avec
// bascule et navigation entre les deux.
package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"gitflow-tui/internal/gitflow"
)

// pollInterval est la fréquence de vérification d'un changement dans le
// dépôt (nouveau commit, fusion, branche créée/supprimée) pour le
// rafraîchissement automatique. Chaque tick ne fait que comparer une
// empreinte bon marché (lecture de fichiers, sans lancer git) ; le
// rechargement complet n'est déclenché que si elle a changé.
const pollInterval = 2 * time.Second

// graphScrollStep est le pas du défilement horizontal de la vue graphe
// (←/→), en caractères : quatre colonnes du diagramme.
const graphScrollStep = 4 * cellWidth

// pollTickMsg déclenche une vérification de l'empreinte du dépôt.
type pollTickMsg struct{}

func pollTick() tea.Cmd {
	return tea.Tick(pollInterval, func(time.Time) tea.Msg { return pollTickMsg{} })
}

// fingerprintMsg porte l'empreinte courante du dépôt, calculée en tâche de
// fond : parcourir les refs sur disque (des milliers en mode remote, ou sur
// un système de fichiers lent) ne doit pas figer l'interface.
type fingerprintMsg string

func checkFingerprint(repo Repository) tea.Cmd {
	return func() tea.Msg { return fingerprintMsg(repo.StateFingerprint()) }
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

// Repository est ce dont l'interface a besoin du dépôt : son historique, à
// analyser, et de quoi afficher un commit et surveiller les changements.
type Repository interface {
	gitflow.History
	// Root renvoie la racine du working tree.
	Root() string
	// Remote renvoie le remote dont les branches sont analysées, vide pour
	// les branches locales.
	Remote() string
	// Show renvoie le contenu complet d'un commit (git show), statistiques
	// calibrées pour width colonnes.
	Show(hash string, width int) (string, error)
	// StateFingerprint renvoie une empreinte bon marché de l'état des refs,
	// obtenue sans lancer git : un changement de valeur signale un commit,
	// une fusion, un changement ou une suppression de branche.
	StateFingerprint() string
}

// column est un groupe de branches du panneau de gauche de la vue
// colonnes (un type GitFlow).
type column struct {
	title string
	items []gitflow.BranchState
}

// branchRow est une ligne du panneau de gauche de la vue colonnes : soit un
// en-tête de groupe (non sélectionnable, avec le nombre de branches
// affichées), soit une branche.
type branchRow struct {
	header string
	count  int
	it     gitflow.BranchState
}

// dataLoadedMsg porte le résultat d'un chargement (initial, manuel via 'r',
// ou automatique en arrière-plan). fingerprint est l'empreinte du dépôt
// telle qu'elle était juste avant les lectures git : la comparer à
// l'empreinte courante lors du tick suivant permet de rattraper tout
// changement survenu pendant le chargement lui-même.
type dataLoadedMsg struct {
	report      gitflow.Report
	columns     []column
	fingerprint string
	err         error
}

// diffLoadedMsg porte le contenu (git show) du commit demandé et, pour une
// fusion, les commits qu'elle a apportés. hash permet d'ignorer un résultat
// devenu obsolète si la sélection a changé entre temps.
type diffLoadedMsg struct {
	hash   string
	merged []gitflow.Commit
	show   string
	err    error
}

// Model est le modèle racine Bubble Tea de gitflow-tui.
type Model struct {
	repo Repository
	opts Options
	// report est le résultat de la dernière analyse du dépôt ; son
	// classifieur porte les conventions GitFlow du dépôt (noms des branches
	// permanentes, préfixes).
	report gitflow.Report

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

	graph viewport.Model
	// graphX est le décalage horizontal (en colonnes de caractères) de la
	// vue graphe, dont les lignes dépassent souvent la largeur du terminal.
	graphX int
	// graphSelKey identifie la ligne de branche sélectionnée dans le graphe
	// (cf. laneKey) ; graphAnchors situe les lignes du dernier rendu.
	graphSelKey  string
	graphAnchors []graphAnchor

	diff    viewport.Model
	diffFor string
	// diffMerged et diffShow gardent le contenu brut du commit affiché, pour
	// le remettre en forme (retour à la ligne) quand la largeur change.
	diffMerged []gitflow.Commit
	diffShow   string

	// loadedAt est l'heure du dernier chargement réussi, affichée dans
	// l'en-tête.
	loadedAt time.Time

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
func New(repo Repository, opts Options) Model {
	return Model{
		repo:              repo,
		opts:              opts,
		view:              viewColumns,
		graph:             viewport.New(0, 0),
		diff:              viewport.New(0, 0),
		loading:           true,
		historyAlertsOnly: true,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(loadData(m.repo, m.opts), pollTick())
}

// Options règle le comportement de l'interface.
type Options struct {
	// StaleAfter est la durée sans commit au-delà de laquelle une branche
	// éphémère pas encore complètement fusionnée est signalée comme
	// inactive ; 0 désactive ce signalement.
	StaleAfter time.Duration
}

// loadData analyse le dépôt en tâche de fond.
func loadData(repo Repository, opts Options) tea.Cmd {
	return func() tea.Msg {
		fingerprint := repo.StateFingerprint()
		report, err := gitflow.Analyze(repo, gitflow.Options{StaleAfter: opts.StaleAfter, Now: time.Now()})
		if err != nil {
			return dataLoadedMsg{fingerprint: fingerprint, err: err}
		}
		return dataLoadedMsg{report: report, columns: branchColumns(report), fingerprint: fingerprint}
	}
}

// branchColumns groupe les branches par type GitFlow pour le panneau de
// gauche de la vue colonnes, titrés d'après les préfixes du dépôt ; main
// puis develop en tête, dans le groupe des permanentes.
func branchColumns(r gitflow.Report) []column {
	cfg := r.Classifier.Config
	groups := []struct {
		title string
		types []gitflow.BranchType
	}{
		{"permanentes", []gitflow.BranchType{gitflow.TypeMain, gitflow.TypeDevelop}},
		{cfg.FeaturePrefix + "*", []gitflow.BranchType{gitflow.TypeFeature}},
		{cfg.BugfixPrefix + "*", []gitflow.BranchType{gitflow.TypeBugfix}},
		{cfg.ReleasePrefix + "*", []gitflow.BranchType{gitflow.TypeRelease}},
		{cfg.HotfixPrefix + "*", []gitflow.BranchType{gitflow.TypeHotfix}},
		{cfg.SupportPrefix + "*", []gitflow.BranchType{gitflow.TypeSupport}},
		{"autre", []gitflow.BranchType{gitflow.TypeOther}},
	}
	cols := make([]column, 0, len(groups))
	for _, g := range groups {
		col := column{title: g.title}
		for _, t := range g.types {
			for _, st := range r.Branches {
				if st.Node.Type == t {
					col.items = append(col.items, st)
				}
			}
		}
		cols = append(cols, col)
	}
	return cols
}

// currentGraphContent rend la vue graphe selon le mode actif, sans refaire
// d'appel git : historique complet, ou seulement l'état courant du dépôt.
func (m Model) currentGraphContent() (string, []graphAnchor) {
	emptyMsg := "Aucune branche feature/release/hotfix, active ou fusionnée."
	switch {
	case m.graphMode == graphLive:
		emptyMsg = "Aucune branche en attente de fusion pour le moment."
	case m.historyAlertsOnly:
		emptyMsg = "Historique propre : aucune déviation du workflow GitFlow détectée."
	}
	g := newGraphData(m.report, m.graphMode, m.historyAlertsOnly)
	return renderGraph(g, emptyMsg, m.graphSelKey)
}

// loadDiff récupère en tâche de fond le contenu complet (git show) du commit
// c. Si c'est une fusion, les commits qu'elle a apportés (souvent
// invisibles dans l'historique premier-parent de main/develop) sont
// récupérés aussi, pour être affichés avant le diff, puisque celui-ci est en
// pratique peu informatif pour une fusion propre (git montre alors un diff
// combiné, généralement vide).
func loadDiff(repo Repository, c gitflow.Commit, width int) tea.Cmd {
	return func() tea.Msg {
		var merged []gitflow.Commit
		if c.IsMerge() {
			if commits, err := repo.CommitsBetween(c.Parents[0], c.Parents[1]); err == nil {
				merged = commits
			}
		}
		show, err := repo.Show(c.Hash, width)
		if err != nil {
			return diffLoadedMsg{hash: c.Hash, err: err}
		}
		return diffLoadedMsg{hash: c.Hash, merged: merged, show: show}
	}
}

// refreshDiff remet en forme le contenu du commit affiché pour la largeur
// actuelle du panneau, sans changer la position de défilement.
func (m *Model) refreshDiff() {
	if m.diffShow == "" {
		return
	}
	m.diff.SetContent(renderDiff(m.diffMerged, m.diffShow, m.diff.Width))
}

// graphLegend résume les symboles du graphe, en tête de la vue.
var graphLegend = styleFaint.Render("● courante  ○ en cours  ✔ fusionnée  × supprimée  ⚠ écart GitFlow  —  ? : légende complète")

// refreshGraph recalcule la vue graphe et la découpe horizontalement selon
// graphX : chaque ligne est tronquée à la largeur du terminal (avec "…")
// plutôt que renvoyée à la ligne par le viewport, ce qui casserait
// l'alignement du diagramme. Au-delà de la marge de gauche, figée (noms des
// lignes permanentes, année), le diagramme défile de graphX colonnes.
func (m *Model) refreshGraph() {
	content, anchors := m.currentGraphContent()
	// La ligne sélectionnée a pu disparaître (filtre, rechargement) : se
	// rabattre sur la première, puis refaire le rendu pour la surligner.
	if len(anchors) > 0 && m.graphAnchorIndex(anchors) < 0 {
		m.graphSelKey = laneKey(anchors[0].lane)
		content, anchors = m.currentGraphContent()
	}
	m.graphAnchors = anchors
	lines := strings.Split(content, "\n")
	widest := 0
	for _, l := range lines {
		widest = maxInt(widest, ansi.StringWidth(l))
	}
	width := maxInt(m.graph.Width, 1)
	m.graphX = maxInt(0, minInt(m.graphX, widest-width))
	for i, l := range lines {
		if m.graphX > 0 {
			margin := ansi.Truncate(l, laneLabelWidth, "")
			margin += strings.Repeat(" ", laneLabelWidth-ansi.StringWidth(margin))
			l = margin + cutLeft(l, laneLabelWidth+m.graphX)
		}
		lines[i] = ansi.Truncate(l, width, "…")
	}
	legend := ansi.Truncate(graphLegend, width, "…")
	m.graph.SetContent(legend + "\n\n" + strings.Join(lines, "\n"))

	// Faire défiler juste ce qu'il faut pour garder la ligne sélectionnée
	// visible (la légende et la ligne vide qui la suit la décalent de 2).
	if i := m.graphAnchorIndex(anchors); i >= 0 {
		line := anchors[i].line + 2
		switch {
		case line < m.graph.YOffset:
			m.graph.SetYOffset(line)
		case line >= m.graph.YOffset+m.graph.Height:
			m.graph.SetYOffset(line - m.graph.Height + 1)
		}
	}
}

// graphAnchorIndex renvoie la position de la ligne sélectionnée parmi
// anchors, ou -1.
func (m Model) graphAnchorIndex(anchors []graphAnchor) int {
	for i, a := range anchors {
		if laneKey(a.lane) == m.graphSelKey {
			return i
		}
	}
	return -1
}

// moveGraphSelection sélectionne la ligne de branche d vers le bas (ou vers
// le haut si d < 0) dans le graphe, sans boucler.
func (m *Model) moveGraphSelection(d int) {
	if len(m.graphAnchors) == 0 {
		return
	}
	i := m.graphAnchorIndex(m.graphAnchors) + d
	i = maxInt(0, minInt(i, len(m.graphAnchors)-1))
	m.graphSelKey = laneKey(m.graphAnchors[i].lane)
	m.refreshGraph()
}

// openGraphSelection ouvre la ligne sélectionnée du graphe dans la vue
// colonnes : la branche elle-même si elle existe encore, sinon (branche
// supprimée, synchronisation) son commit de fusion, dans la branche qui
// l'a reçue.
func (m *Model) openGraphSelection() tea.Cmd {
	i := m.graphAnchorIndex(m.graphAnchors)
	if i < 0 {
		return nil
	}
	l := m.graphAnchors[i].lane
	if l.Kind == gitflow.LaneLive {
		return m.openBranch(l.Node.Name, "")
	}
	for _, refs := range [][]gitflow.MergeRef{l.Merged, l.Unexpected} {
		for _, mr := range refs {
			if mr.Hash != "" {
				return m.openBranch(mr.Target, mr.Hash)
			}
		}
	}
	return nil
}

// openBranch bascule sur la vue colonnes, la branche name sélectionnée
// (filtre levé au besoin pour qu'elle soit visible) et, si commitHash est
// renseigné, ce commit sélectionné dans sa liste, focus sur les commits.
func (m *Model) openBranch(name, commitHash string) tea.Cmd {
	m.filter = ""
	m.view = viewColumns
	m.paneFocus = paneCommits
	m.selectedBranchName = name
	m.restoreBranchSelection()
	m.commitIdx = 0
	if it, ok := m.selectedBranchRow(); ok && commitHash != "" {
		for i, c := range it.Commits {
			if c.Hash == commitHash {
				m.commitIdx = i
				break
			}
		}
	}
	m.syncSelectedCommitHash()
	return m.syncDiff()
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
		m.refreshDiff()
		m.refreshGraph()
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
		m.report = msg.report
		m.loadedAt = time.Now()
		m.refreshGraph()
		m.restoreBranchSelection()
		m.restoreCommitSelection()
		return m, m.syncDiff()

	case pollTickMsg:
		// Une seule vérification à la fois : le tick suivant n'est programmé
		// qu'une fois celle-ci terminée (fingerprintMsg).
		if m.loading || m.refreshing {
			return m, pollTick()
		}
		return m, checkFingerprint(m.repo)

	case fingerprintMsg:
		next := pollTick()
		if m.loading || m.refreshing || string(msg) == m.lastFingerprint {
			return m, next
		}
		m.refreshing = true
		return m, tea.Batch(next, loadData(m.repo, m.opts))

	case diffLoadedMsg:
		if msg.hash != m.diffFor {
			return m, nil
		}
		if msg.err != nil {
			m.diff.SetContent(styleFaint.Render("Erreur : " + msg.err.Error()))
		} else {
			m.diffMerged, m.diffShow = msg.merged, msg.show
			m.refreshDiff()
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
		return m, loadData(m.repo, m.opts)
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
			m.refreshGraph()
		}
		return m, nil
	case key.Matches(msg, keys.Alerts):
		if m.view == viewGraph && m.graphMode == graphHistory {
			m.historyAlertsOnly = !m.historyAlertsOnly
			m.refreshGraph()
		}
		return m, nil
	case key.Matches(msg, keys.Filter):
		if m.view == viewColumns {
			m.filtering = true
		}
		return m, nil
	}

	if m.view == viewGraph {
		switch {
		case key.Matches(msg, keys.Left):
			m.graphX -= graphScrollStep
			m.refreshGraph()
			return m, nil
		case key.Matches(msg, keys.Right):
			m.graphX += graphScrollStep
			m.refreshGraph()
			return m, nil
		case key.Matches(msg, keys.Up) && len(m.graphAnchors) > 0:
			m.moveGraphSelection(-1)
			return m, nil
		case key.Matches(msg, keys.Down) && len(m.graphAnchors) > 0:
			m.moveGraphSelection(1)
			return m, nil
		case key.Matches(msg, keys.Open):
			return m, m.openGraphSelection()
		}
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
	n := len(it.Commits)
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
		m.diffMerged, m.diffShow = nil, ""
		m.diff.SetContent(styleFaint.Render("Aucun commit sélectionné."))
		m.diff.GotoTop()
		return nil
	}
	if c.Hash == m.diffFor {
		return nil
	}
	m.diffFor = c.Hash
	m.diffMerged, m.diffShow = nil, ""
	m.diff.SetContent(styleFaint.Render("Chargement..."))
	m.diff.GotoTop()
	return loadDiff(m.repo, c, m.diff.Width)
}

func selectableRow(rows []branchRow, i int) bool {
	return i >= 0 && i < len(rows) && rows[i].header == ""
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
			if selectableRow(rows, i) && row.it.Branch.IsHead {
				m.branchIdx = i
				m.syncSelectedBranchName()
				return
			}
		}
	}
	if m.selectedBranchName != "" {
		for i, row := range rows {
			if selectableRow(rows, i) && row.it.Node.Name == m.selectedBranchName {
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
		m.selectedBranchName = rows[m.branchIdx].it.Node.Name
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
		for i, c := range it.Commits {
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

func (m Model) visibleItems(col column) []gitflow.BranchState {
	if m.filter == "" {
		return col.items
	}
	needle := strings.ToLower(m.filter)
	out := make([]gitflow.BranchState, 0, len(col.items))
	for _, it := range col.items {
		if strings.Contains(strings.ToLower(it.Node.Name), needle) {
			out = append(out, it)
		}
	}
	return out
}

// flatBranches aplatit les colonnes groupées par type GitFlow en une liste
// unique de lignes pour le panneau de gauche de la vue colonnes : un en-tête
// par groupe (avec son nombre de branches), puis ses branches. Les groupes
// sans branche — ou sans résultat pour le filtre en cours — sont omis.
func (m Model) flatBranches() []branchRow {
	var rows []branchRow
	for _, col := range m.columns {
		items := m.visibleItems(col)
		if len(items) == 0 {
			continue
		}
		rows = append(rows, branchRow{header: col.title, count: len(items)})
		for _, it := range items {
			rows = append(rows, branchRow{it: it})
		}
	}
	return rows
}

func (m Model) selectedBranchRow() (gitflow.BranchState, bool) {
	rows := m.flatBranches()
	if !selectableRow(rows, m.branchIdx) {
		return gitflow.BranchState{}, false
	}
	return rows[m.branchIdx].it, true
}

func (m Model) selectedCommit() (gitflow.Commit, bool) {
	it, ok := m.selectedBranchRow()
	if !ok || len(it.Commits) == 0 {
		return gitflow.Commit{}, false
	}
	idx := m.commitIdx
	if idx < 0 || idx >= len(it.Commits) {
		idx = 0
	}
	return it.Commits[idx], true
}

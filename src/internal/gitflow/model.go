package gitflow

// Commit est un commit tel que l'analyse GitFlow le manipule.
type Commit struct {
	Hash       string   // hash complet
	Parents    []string // hashes complets des parents, premier parent en tête
	Author     string
	AuthorDate string // AAAA-MM-JJ, pour l'affichage
	CommitDate string // ISO 8601, fuseau local : ordonne les événements
	Subject    string
}

// Short renvoie le hash abrégé, pour l'affichage.
func (c Commit) Short() string {
	if len(c.Hash) > 7 {
		return c.Hash[:7]
	}
	return c.Hash
}

// IsMerge indique si le commit a plus d'un parent (commit de fusion).
func (c Commit) IsMerge() bool {
	return len(c.Parents) > 1
}

// Branch est une branche analysée (locale, ou d'un remote).
type Branch struct {
	Name       string
	CommitDate string // date du dernier commit, même format que Commit.CommitDate
	IsHead     bool   // branche actuellement extraite
}

// MergeRef décrit l'intégration d'une branche dans une cible : le commit de
// fusion quand il a pu être identifié dans l'historique (Hash, Date, Base,
// Tip), ou seulement la cible quand on sait uniquement qu'elle contient le
// travail de la branche (fusion en fast-forward, ou arrivée par un autre
// chemin).
type MergeRef struct {
	Target string
	Date   string // date du commit de fusion (vide si non identifié)
	Hash   string // commit de fusion (vide si non identifié)
	Base   string // premier parent de la fusion : la cible avant intégration
	Tip    string // second parent : la pointe de la branche intégrée
	Tag    string // tag de version posé sur cette fusion (cf. checkVersionTags)
}

// LaneKind distingue l'origine d'une ligne de l'historique GitFlow.
type LaneKind int

const (
	LaneLive    LaneKind = iota // branche encore présente
	LaneDeleted                 // branche supprimée, reconstituée depuis les messages de fusion
	LaneSync                    // fusion directe develop → main
)

// MergeStatus résume où en est l'intégration d'une branche dans ses cibles.
type MergeStatus int

const (
	StatusInProgress MergeStatus = iota // aucune cible atteinte
	StatusMerged                        // toutes les cibles atteintes
	StatusPartial                       // certaines cibles atteintes, pas toutes
)

// Lane est une branche de travail (ou une synchronisation develop → main)
// replacée dans l'historique GitFlow : ses fusions, ce qui lui manque, et
// ses écarts au workflow.
type Lane struct {
	Node   Node
	Branch Branch // vide pour une branche supprimée ou une synchronisation
	Kind   LaneKind
	// Date place la ligne dans le temps : sa première intégration connue,
	// sinon son point de divergence (branche pas encore fusionnée).
	Date       string
	Merged     []MergeRef // intégrations dans des cibles prévues par GitFlow
	Pending    []string   // cibles prévues jamais atteintes
	Unexpected []MergeRef // fusions vers des cibles non prévues
	// WrongParent nomme l'autre branche permanente quand la branche semble
	// en être partie plutôt que de son parent attendu.
	WrongParent string
	// UntaggedIn nomme la branche principale quand la fusion de cette
	// release ou de ce hotfix n'y porte pas de tag de version, dans un
	// dépôt qui étiquette pourtant ses versions.
	UntaggedIn string
	// StaleDays est le nombre de jours sans commit d'une branche pas encore
	// complètement fusionnée, au-delà du seuil d'inactivité ; 0 sinon.
	StaleDays int
	Commits   []Commit // travail propre de la branche, du plus récent au plus ancien
}

// Status renvoie l'état d'intégration de la ligne dans ses cibles.
func (l Lane) Status() MergeStatus {
	switch {
	case len(l.Merged) > 0 && len(l.Pending) == 0:
		return StatusMerged
	case len(l.Merged) > 0:
		return StatusPartial
	}
	return StatusInProgress
}

// IsPending indique si la ligne est du travail en cours : une branche
// encore présente qui n'a pas atteint toutes ses cibles.
func (l Lane) IsPending() bool {
	return l.Kind == LaneLive && len(l.Pending) > 0
}

// TargetsMain indique si la ligne se rattache à la branche principale
// plutôt qu'à develop. Le critère est la cible de fusion attendue, pas
// l'origine : une release part de develop mais son but est d'atterrir dans
// main, comme les hotfix et les synchronisations develop → main. Seules
// feature et bugfix, qui ne doivent fusionner que dans develop, vont avec
// develop.
func (l Lane) TargetsMain() bool {
	switch l.Node.Type {
	case TypeRelease, TypeHotfix:
		return true
	case TypeFeature, TypeBugfix:
		return false
	}
	return l.Kind == LaneSync
}

// Spine est une branche permanente (main ou develop) et sa ligne directe.
type Spine struct {
	Branch Branch
	// Log est la ligne directe (premier parent), du plus récent au plus
	// ancien : ce qui a été commité ou fusionné sur la branche.
	Log []Commit
	// Direct liste les commits arrivés directement sur la branche, hors
	// fusion, une fois le flow amorcé (cf. directCommits).
	Direct []Commit
}

// BranchState est l'état d'une branche existante, pour la liste des
// branches.
type BranchState struct {
	Node   Node
	Branch Branch
	// Ahead et Behind comparent la branche à sa parente GitFlow : commits
	// qu'elle seule a, commits de la parente qu'elle n'a pas. Pour develop,
	// la comparaison se fait avec main, commits de fusion exclus.
	Ahead, Behind int
	Status        MergeStatus // pour une branche éphémère
	StaleDays     int
	// Commits est la ligne directe pour main et develop, le travail propre
	// de la branche pour les autres.
	Commits []Commit
}

// Report est le résultat de l'analyse GitFlow d'un dépôt.
type Report struct {
	Classifier Classifier
	Main       *Spine // nil si la branche principale n'existe pas
	Develop    *Spine // nil si develop n'existe pas
	// Branches liste les branches existantes, triées par nom.
	Branches []BranchState
	// Lanes liste les branches de travail, présentes ou reconstituées, et
	// les synchronisations develop → main, dans l'ordre chronologique.
	Lanes []Lane
	// Unreleased est le nombre de commits (hors fusions) de develop pas
	// encore arrivés dans main : le contenu de la prochaine release.
	Unreleased    int
	CurrentBranch string // vide si HEAD est détachée
}

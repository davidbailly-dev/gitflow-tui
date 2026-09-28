// Package gitflow contient la logique pure de classification des branches
// selon la convention GitFlow (aucun appel git ici, uniquement des noms de
// branches).
package gitflow

import "strings"

// BranchType identifie le rôle d'une branche dans le modèle GitFlow.
type BranchType int

const (
	TypeMain BranchType = iota
	TypeDevelop
	TypeFeature
	TypeBugfix
	TypeRelease
	TypeHotfix
	TypeSupport
	TypeOther
)

// Node décrit une branche classifiée et ses relations dans le flow.
type Node struct {
	Name         string
	Type         BranchType
	Parent       string   // branche dont elle part conceptuellement
	MergeTargets []string // branches dans lesquelles elle doit être fusionnée
}

// IsEphemeral indique si le type correspond à une branche de travail
// temporaire (feature, bugfix, release, hotfix), destinée à être fusionnée
// puis supprimée. Une branche support, qui maintient une ancienne version
// sans jamais être refusionnée, n'en est pas une.
func (t BranchType) IsEphemeral() bool {
	return t == TypeFeature || t == TypeBugfix || t == TypeRelease || t == TypeHotfix
}

// Classifier applique les règles GitFlow en connaissant les noms réels des
// deux branches permanentes du dépôt et ses préfixes de branches : parents
// et cibles de fusion des branches éphémères en dépendent (un hotfix part
// de "master" sur un dépôt qui n'a pas de "main").
type Classifier struct {
	Main    string
	Develop string
	Config  Config
}

// NewClassifier applique cfg aux branches existantes. Si cfg ne fixe pas la
// branche principale, elle est déduite des branches existantes : "main" si
// elle existe, sinon "master" ; "main" par convention si aucune des deux
// n'existe.
func NewClassifier(names []string, cfg Config) Classifier {
	c := Classifier{Main: cfg.Main, Develop: cfg.Develop, Config: cfg}
	if c.Develop == "" {
		c.Develop = "develop"
	}
	if c.Main != "" {
		return c
	}

	hasMain, hasMaster := false, false
	for _, name := range names {
		switch name {
		case "main":
			hasMain = true
		case "master":
			hasMaster = true
		}
	}
	c.Main = "main"
	if !hasMain && hasMaster {
		c.Main = "master"
	}
	return c
}

// Classify détermine le type et les relations de chaque branche d'après son
// nom, en appliquant les règles GitFlow.
func (c Classifier) Classify(names []string) []Node {
	nodes := make([]Node, 0, len(names))
	for _, name := range names {
		nodes = append(nodes, c.ClassifyOne(name))
	}
	return nodes
}

// ClassifyOne classifie une seule branche. Seule la branche principale
// retenue par le classifieur est de type TypeMain : si "main" et "master"
// coexistent, "master" est rangée parmi les autres branches.
func (c Classifier) ClassifyOne(name string) Node {
	has := func(prefix string) bool { return prefix != "" && strings.HasPrefix(name, prefix) }
	switch {
	case name == c.Main:
		return Node{Name: name, Type: TypeMain}
	case name == c.Develop:
		return Node{Name: name, Type: TypeDevelop}
	case has(c.Config.FeaturePrefix):
		return Node{Name: name, Type: TypeFeature, Parent: c.Develop, MergeTargets: []string{c.Develop}}
	case has(c.Config.BugfixPrefix):
		return Node{Name: name, Type: TypeBugfix, Parent: c.Develop, MergeTargets: []string{c.Develop}}
	case has(c.Config.ReleasePrefix):
		return Node{Name: name, Type: TypeRelease, Parent: c.Develop, MergeTargets: []string{c.Develop, c.Main}}
	case has(c.Config.HotfixPrefix):
		return Node{Name: name, Type: TypeHotfix, Parent: c.Main, MergeTargets: []string{c.Main, c.Develop}}
	case has(c.Config.SupportPrefix):
		return Node{Name: name, Type: TypeSupport, Parent: c.Main}
	default:
		return Node{Name: name, Type: TypeOther}
	}
}

// Classify classifie names avec les conventions par défaut, la branche
// principale étant déduite de ces mêmes noms.
func Classify(names []string) []Node {
	return NewClassifier(names, DefaultConfig()).Classify(names)
}

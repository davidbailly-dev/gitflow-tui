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
	TypeRelease
	TypeHotfix
	TypeOther
)

// Node décrit une branche classifiée et ses relations dans le flow.
type Node struct {
	Name         string
	Type         BranchType
	Parent       string   // branche dont elle part conceptuellement
	MergeTargets []string // branches dans lesquelles elle doit être fusionnée
}

// Classify détermine le type et les relations de chaque branche d'après son
// nom, en appliquant les règles GitFlow.
func Classify(names []string) []Node {
	nodes := make([]Node, 0, len(names))
	for _, name := range names {
		nodes = append(nodes, classifyOne(name))
	}
	return nodes
}

func classifyOne(name string) Node {
	switch {
	case name == "main" || name == "master":
		return Node{Name: name, Type: TypeMain}
	case name == "develop":
		return Node{Name: name, Type: TypeDevelop}
	case strings.HasPrefix(name, "feature/"):
		return Node{Name: name, Type: TypeFeature, Parent: "develop", MergeTargets: []string{"develop"}}
	case strings.HasPrefix(name, "release/"):
		return Node{Name: name, Type: TypeRelease, Parent: "develop", MergeTargets: []string{"develop", "main"}}
	case strings.HasPrefix(name, "hotfix/"):
		return Node{Name: name, Type: TypeHotfix, Parent: "main", MergeTargets: []string{"main", "develop"}}
	default:
		return Node{Name: name, Type: TypeOther}
	}
}

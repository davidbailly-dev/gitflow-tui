package tui

import (
	"gitflow-tui/internal/git"
	"gitflow-tui/internal/gitflow"
)

// spines donne, pour chaque branche permanente présente (main, develop),
// l'ensemble des hash complets de sa ligne directe (premier parent) : les
// commits qui y ont été créés ou fusionnés, par opposition aux commits des
// branches qu'elle a intégrées.
type spines map[string]map[string]bool

func loadSpines(repo git.Repository, names ...string) spines {
	sp := make(spines, len(names))
	for _, name := range names {
		hashes, err := repo.FirstParentHashes(name)
		if err != nil {
			continue
		}
		set := make(map[string]bool, len(hashes))
		for _, h := range hashes {
			set[h] = true
		}
		sp[name] = set
	}
	return sp
}

// nearestMatch renvoie la position, dans chain (une chaîne "premier parent"
// classée du plus récent au plus ancien), du premier commit appartenant à
// l'un des ensembles sets — ou -1 si aucun ne correspond.
func nearestMatch(chain []string, sets ...map[string]bool) int {
	for i, h := range chain {
		for _, set := range sets {
			if set[h] {
				return i
			}
		}
	}
	return -1
}

// divergence trouve le vrai point de divergence de n : le premier commit de
// sa propre ligne directe (en partant de la pointe) qui appartient aussi à
// l'une des deux lignes permanentes — pas un simple ancêtre commun lointain
// (une fois la branche fusionnée partout, un merge-base avec n'importe
// quelle branche renvoie trivialement sa propre pointe). Si ce point est
// plus proche de la pointe côté de l'autre branche permanente que côté du
// parent attendu, la branche a probablement été créée depuis cette autre
// branche : wrongParent en donne alors le nom. fork est vide si aucun point
// de divergence n'a été trouvé.
func divergence(repo git.Repository, n gitflow.Node, classifier gitflow.Classifier, sp spines) (fork, wrongParent string) {
	chain, err := repo.FirstParentHashes(n.Name)
	if err != nil {
		return "", ""
	}
	other := classifier.Main
	if n.Parent == classifier.Main {
		other = classifier.Develop
	}
	idxExpected := nearestMatch(chain, sp[n.Parent])
	idxOther := nearestMatch(chain, sp[other])
	switch {
	case idxOther >= 0 && (idxExpected < 0 || idxOther < idxExpected):
		return chain[idxOther], other
	case idxExpected >= 0:
		return chain[idxExpected], ""
	}
	return "", ""
}

// ownCommits renvoie les commits propres à la branche n, du plus récent au
// plus ancien : ceux qui ne sont pas encore dans sa branche parente (ou,
// pour une branche hors GitFlow, ni dans main ni dans develop). Une fois la
// branche fusionnée, sa parente contient tous ses commits et cette
// différence est vide : ses commits sont alors retrouvés en remontant sa
// ligne directe jusqu'à son point de divergence avec les lignes
// permanentes, pour qu'une branche fusionnée mais conservée affiche son
// travail comme une branche fusionnée puis supprimée.
func ownCommits(repo git.Repository, n gitflow.Node, sp spines) []git.Commit {
	var bases []string
	if n.Parent != "" {
		bases = []string{n.Parent}
	} else {
		for name := range sp {
			bases = append(bases, name)
		}
	}
	if len(bases) == 0 {
		return nil
	}

	if commits, err := repo.CommitsNotIn(n.Name, bases...); err == nil && len(commits) > 0 {
		return commits
	}

	chain, err := repo.FirstParentHashes(n.Name)
	if err != nil {
		return nil
	}
	sets := make([]map[string]bool, 0, len(bases))
	for _, base := range bases {
		sets = append(sets, sp[base])
	}
	idx := nearestMatch(chain, sets...)
	if idx <= 0 {
		// Pointe déjà sur une ligne permanente : branche tout juste créée,
		// sans commit propre (ou intégrée en fast-forward, indiscernable).
		return nil
	}
	commits, err := repo.FirstParentCommitsSince(n.Name, chain[idx])
	if err != nil {
		return nil
	}
	return commits
}

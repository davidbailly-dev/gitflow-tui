package git

import (
	"strings"

	"gitflow-tui/internal/gitflow"
)

// branchFormat sépare ses champs par le caractère de contrôle US (%1f),
// qui ne peut pas apparaître dans un nom de ref (contrairement à "|").
const branchFormat = "%(refname)%1f%(committerdate:iso-local)"

// Branches renvoie les branches analysées : locales, ou celles du remote
// choisi à l'ouverture (sans son "HEAD" symbolique), sous leur nom court.
func (r *Repository) Branches() ([]gitflow.Branch, error) {
	prefix := r.branchRefPrefix()
	out, err := r.run("for-each-ref", "--format="+branchFormat, prefix)
	if err != nil {
		return nil, err
	}
	current := r.currentBranch()

	var branches []gitflow.Branch
	names := make(map[string]bool)
	for _, line := range strings.Split(out, "\n") {
		ref, date, ok := strings.Cut(line, "\x1f")
		if !ok {
			continue
		}
		name := strings.TrimPrefix(ref, prefix)
		if name == "HEAD" {
			continue
		}
		names[name] = true
		branches = append(branches, gitflow.Branch{Name: name, CommitDate: date, IsHead: name == current})
	}

	r.mu.Lock()
	r.branchNames = names
	r.mu.Unlock()
	return branches, nil
}

// currentBranch renvoie le nom court de la branche analysée qui correspond
// à la branche extraite : elle-même pour les branches locales ; en mode
// remote, la branche du remote qu'elle suit (@{u}), ou rien si elle n'en
// suit aucune de ce remote — une branche distante homonyme n'est pas pour
// autant la branche courante.
func (r *Repository) currentBranch() string {
	if r.remote == "" {
		current, _ := r.run("branch", "--show-current")
		return current
	}
	upstream, err := r.run("rev-parse", "--symbolic-full-name", "@{u}")
	if err != nil || !strings.HasPrefix(upstream, r.branchRefPrefix()) {
		return ""
	}
	return strings.TrimPrefix(upstream, r.branchRefPrefix())
}

// AheadBehind compare branch à base : commits propres à branch, et commits
// de base absents de branch.
func (r *Repository) AheadBehind(base, branch string) (int, int, error) {
	out, err := r.run("rev-list", "--left-right", "--count", r.ref(base)+"..."+r.ref(branch))
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0, nil
	}
	return atoiSafe(fields[1]), atoiSafe(fields[0]), nil
}

// CountNotIn compte les commits de branch absents de base, commits de
// fusion exclus.
func (r *Repository) CountNotIn(branch, base string) (int, error) {
	out, err := r.run("rev-list", "--count", "--no-merges", r.ref(branch), "--not", r.ref(base))
	if err != nil {
		return 0, err
	}
	return atoiSafe(out), nil
}

func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

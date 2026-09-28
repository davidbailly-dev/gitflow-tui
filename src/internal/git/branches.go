package git

import "strings"

// Branch décrit une branche locale telle que rapportée par git.
type Branch struct {
	Name       string
	Head       string // hash court du dernier commit
	CommitDate string // date du dernier commit (committer, fuseau local)
	IsHead     bool
}

const branchFormat = "%(refname)|%(objectname:short)|%(committerdate:iso-local)"

// Branches renvoie les branches analysées : locales, ou celles du remote
// choisi à l'ouverture (sans son "HEAD" symbolique), sous leur nom court.
func (r *execRepository) Branches() ([]Branch, error) {
	prefix := r.branchRefPrefix()
	out, err := r.run("for-each-ref", "--format="+branchFormat, prefix)
	if err != nil {
		return nil, err
	}
	current, _ := r.CurrentBranch()

	var branches []Branch
	names := make(map[string]bool)
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			continue
		}
		name := strings.TrimPrefix(parts[0], prefix)
		if name == "HEAD" {
			continue
		}
		names[name] = true
		branches = append(branches, Branch{
			Name:       name,
			Head:       parts[1],
			CommitDate: parts[2],
			IsHead:     name == current,
		})
	}

	r.mu.Lock()
	r.branchNames = names
	r.mu.Unlock()
	return branches, nil
}

func (r *execRepository) AheadBehind(base, branch string) (int, int, error) {
	out, err := r.run("rev-list", "--left-right", "--count", r.ref(base)+"..."+r.ref(branch))
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0, nil
	}
	behind, ahead := atoiSafe(fields[0]), atoiSafe(fields[1])
	return ahead, behind, nil
}

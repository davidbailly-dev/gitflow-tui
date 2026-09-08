package git

import "strings"

// Branch décrit une branche locale telle que rapportée par git.
type Branch struct {
	Name       string
	Head       string // hash court du dernier commit
	CommitDate string
	Upstream   string // branche distante suivie, vide si aucune
	IsHead     bool
}

const branchFormat = "%(refname:short)|%(objectname:short)|%(committerdate:iso8601)|%(upstream:short)"

func (r *execRepository) Branches() ([]Branch, error) {
	out, err := r.run("for-each-ref", "--format="+branchFormat, "refs/heads")
	if err != nil {
		return nil, err
	}
	current, _ := r.CurrentBranch()

	var branches []Branch
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) != 4 {
			continue
		}
		branches = append(branches, Branch{
			Name:       parts[0],
			Head:       parts[1],
			CommitDate: parts[2],
			Upstream:   parts[3],
			IsHead:     parts[0] == current,
		})
	}
	return branches, nil
}

func (r *execRepository) AheadBehind(base, branch string) (int, int, error) {
	out, err := r.run("rev-list", "--left-right", "--count", base+"..."+branch)
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

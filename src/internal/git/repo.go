// Package git donne accès en lecture aux données d'un dépôt git en s'appuyant
// sur le binaire git (via os/exec) plutôt que de réimplémenter le format git.
package git

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrNotARepo est renvoyée par Open quand le répertoire donné n'est pas (ou
// n'est pas situé dans) un dépôt git.
var ErrNotARepo = errors.New("not a git repository")

// Repository est l'accès en lecture seule aux données d'un dépôt git.
// L'interface permet de substituer une implémentation de test dans les
// paquets qui en dépendent.
type Repository interface {
	CurrentBranch() (string, error)
	Branches() ([]Branch, error)
	AheadBehind(base, branch string) (ahead, behind int, err error)
	CommitsNotIn(branch, base string) ([]Commit, error)
	MergeCommits(mergeHash string) ([]Commit, error)
	LogSubjects(ref string) (string, error)
	DirectCommits(ref string) ([]Commit, error)
	FirstParentHashes(ref string) ([]string, error)
	FirstParentCommits(ref string) ([]Commit, error)
	Show(hash string) (string, error)
	MergeBase(a, b string) (string, error)
	CommitDate(ref string) (string, error)
	IsAncestor(ancestor, descendant string) (bool, error)
}

type execRepository struct {
	dir string
}

// Open détecte le dépôt git contenant dir (racine du dépôt) et renvoie un
// Repository prêt à l'emploi. Elle renvoie ErrNotARepo si dir n'est pas dans
// un dépôt git.
func Open(dir string) (Repository, error) {
	repo := &execRepository{dir: dir}
	top, err := repo.run("rev-parse", "--show-toplevel")
	if err != nil {
		return nil, ErrNotARepo
	}
	repo.dir = top
	return repo, nil
}

func (r *execRepository) run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func (r *execRepository) CurrentBranch() (string, error) {
	return r.run("branch", "--show-current")
}

func (r *execRepository) MergeBase(a, b string) (string, error) {
	return r.run("merge-base", a, b)
}

func (r *execRepository) CommitDate(ref string) (string, error) {
	return r.run("log", "-1", "--date=iso8601", "--pretty=format:%ad", ref)
}

// IsAncestor renvoie true si ancestor est un ancêtre de descendant (ou lui
// est identique), c'est-à-dire si descendant contient déjà tout le travail
// d'ancestor — le signe qu'une branche a été fusionnée.
func (r *execRepository) IsAncestor(ancestor, descendant string) (bool, error) {
	cmd := exec.Command("git", "merge-base", "--is-ancestor", ancestor, descendant)
	cmd.Dir = r.dir
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git merge-base --is-ancestor %s %s: %w", ancestor, descendant, err)
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

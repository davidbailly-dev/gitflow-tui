// Package git donne accès en lecture aux données d'un dépôt git en s'appuyant
// sur le binaire git (via os/exec) plutôt que de réimplémenter le format git.
package git

import (
	"errors"
	"fmt"
	"hash/fnv"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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

	// StateFingerprint renvoie une empreinte bon marché de l'état des refs
	// (HEAD, branches, refs empaquetées), obtenue par simple lecture de
	// fichiers plutôt qu'en lançant git. Un changement de valeur signale un
	// commit, une fusion, un changement de branche, ou une branche créée ou
	// supprimée — utilisée pour détecter un changement à intervalle
	// rapproché sans le coût d'un appel git.
	StateFingerprint() string
}

type execRepository struct {
	dir    string // racine du dépôt (working tree)
	gitDir string // répertoire .git réel, résolu une fois (cf. worktrees)
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

	gitDir, err := repo.run("rev-parse", "--git-dir")
	if err != nil {
		return nil, ErrNotARepo
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(repo.dir, gitDir)
	}
	repo.gitDir = gitDir

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

// StateFingerprint lit HEAD, packed-refs et l'arborescence refs/heads
// directement sur disque (aucun processus git lancé) et en combine une
// empreinte : n'importe quel commit, fusion, changement de branche, ou
// création/suppression de branche modifie l'un de ces fichiers et fait donc
// varier la valeur renvoyée.
func (r *execRepository) StateFingerprint() string {
	h := fnv.New64a()

	if head, err := os.ReadFile(filepath.Join(r.gitDir, "HEAD")); err == nil {
		h.Write(head)
	}

	if info, err := os.Stat(filepath.Join(r.gitDir, "packed-refs")); err == nil {
		fmt.Fprintf(h, "packed-refs:%d:%d", info.Size(), info.ModTime().UnixNano())
	}

	refsHeads := filepath.Join(r.gitDir, "refs", "heads")
	var entries []string
	_ = filepath.WalkDir(refsHeads, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(refsHeads, path)
		if err != nil {
			rel = path
		}
		entries = append(entries, fmt.Sprintf("%s:%d:%d", rel, info.Size(), info.ModTime().UnixNano()))
		return nil
	})
	sort.Strings(entries)
	for _, e := range entries {
		h.Write([]byte(e))
	}

	return fmt.Sprintf("%x", h.Sum64())
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

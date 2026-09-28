// Package git donne accès en lecture aux données d'un dépôt git en s'appuyant
// sur le binaire git (via os/exec) plutôt que de réimplémenter le format git.
package git

import (
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ErrNotARepo est renvoyée par Open quand le répertoire donné n'est pas (ou
// n'est pas situé dans) un dépôt git.
var ErrNotARepo = errors.New("not a git repository")

// Repository est l'accès en lecture seule aux données d'un dépôt git.
// L'interface permet de substituer une implémentation de test dans les
// paquets qui en dépendent.
type Repository interface {
	// Root renvoie la racine du working tree.
	Root() string
	CurrentBranch() (string, error)
	Branches() ([]Branch, error)
	AheadBehind(base, branch string) (ahead, behind int, err error)
	CommitsNotIn(branch string, bases ...string) ([]Commit, error)
	FirstParentCommitsSince(branch, fork string) ([]Commit, error)
	MergeCommits(mergeHash string) ([]Commit, error)
	MergeSubjects(ref string) (string, error)
	DirectCommits(ref string) ([]Commit, error)
	FirstParentHashes(ref string) ([]string, error)
	FirstParentCommits(ref string) ([]Commit, error)
	Show(hash string, width int) (string, error)
	CommitDate(ref string) (string, error)
	IsAncestor(ancestor, descendant string) (bool, error)
	// CountNotIn compte les commits de branch absents de base, commits de
	// fusion exclus : le travail réellement produit, pas les intégrations.
	CountNotIn(branch, base string) (int, error)
	// ResolveCommit renvoie le hash complet du commit désigné par ref.
	ResolveCommit(ref string) (string, error)
	// Tags renvoie, pour chaque commit étiqueté (hash complet), le nom de
	// ses tags.
	Tags() (map[string][]string, error)
	// GitflowConfig renvoie les réglages de la section [gitflow] de la
	// configuration git (clé complète → valeur), vide si "git flow init"
	// n'a jamais été lancé.
	GitflowConfig() (map[string]string, error)
	// Remote renvoie le remote dont les branches sont analysées, vide pour
	// les branches locales.
	Remote() string

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
	gitDir string // répertoire .git propre au working tree (HEAD), résolu une fois
	// commonDir est le répertoire .git partagé par tous les worktrees, qui
	// contient les refs. Identique à gitDir hors worktree ; pour un worktree
	// lié, gitDir vaut .git/worktrees/<nom>, qui n'a pas de refs/heads.
	commonDir string

	// remote, s'il est renseigné, fait analyser les branches de ce remote
	// (refs/remotes/<remote>/…) au lieu des branches locales. Les noms de
	// branches restent courts (ex. "develop") pour tout le reste de
	// l'application : ref les traduit en refs complètes au moment d'appeler
	// git, d'après branchNames, l'ensemble des noms renvoyés par Branches.
	remote      string
	mu          sync.RWMutex
	branchNames map[string]bool
}

// Open détecte le dépôt git contenant dir (racine du dépôt) et renvoie un
// Repository prêt à l'emploi. Si remote est renseigné, ce sont les branches
// de ce remote qui sont analysées plutôt que les branches locales. Open
// renvoie ErrNotARepo si dir n'est pas dans un dépôt git.
func Open(dir, remote string) (Repository, error) {
	repo := &execRepository{dir: dir, remote: remote}
	top, err := repo.run("rev-parse", "--show-toplevel")
	if err != nil {
		return nil, ErrNotARepo
	}
	repo.dir = top

	gitDir, err := repo.run("rev-parse", "--git-dir")
	if err != nil {
		return nil, ErrNotARepo
	}
	repo.gitDir = repo.absPath(gitDir)

	repo.commonDir = repo.gitDir
	if commonDir, err := repo.run("rev-parse", "--git-common-dir"); err == nil {
		repo.commonDir = repo.absPath(commonDir)
	}

	if remote != "" {
		if _, err := repo.run("remote", "get-url", remote); err != nil {
			return nil, fmt.Errorf("remote %q introuvable", remote)
		}
	}

	return repo, nil
}

// branchRefPrefix est le préfixe des refs des branches analysées.
func (r *execRepository) branchRefPrefix() string {
	if r.remote != "" {
		return "refs/remotes/" + r.remote + "/"
	}
	return "refs/heads/"
}

// ref traduit un nom de branche connu en ref complète (ex. "develop" →
// "refs/remotes/origin/develop" en mode remote), pour que git désigne sans
// ambiguïté la branche analysée. Tout autre argument (hash, "hash^2"…) est
// renvoyé tel quel.
func (r *execRepository) ref(name string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.branchNames[name] {
		return r.branchRefPrefix() + name
	}
	return name
}

func (r *execRepository) refs(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = r.ref(n)
	}
	return out
}

func (r *execRepository) Remote() string {
	return r.remote
}

// absPath résout un chemin renvoyé par git rev-parse, relatif à la racine
// du dépôt (répertoire depuis lequel git est lancé).
func (r *execRepository) absPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(r.dir, path)
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

func (r *execRepository) Root() string {
	return r.dir
}

func (r *execRepository) CurrentBranch() (string, error) {
	return r.run("branch", "--show-current")
}

// CommitDate renvoie la date de commit de ref, dans le fuseau local : toutes
// les dates servant à ordonner les événements (branches, fusions) partagent
// ce format, ce qui permet de les comparer comme de simples chaînes.
func (r *execRepository) CommitDate(ref string) (string, error) {
	return r.run("log", "-1", "--date=iso-local", "--pretty=format:%cd", r.ref(ref))
}

func (r *execRepository) CountNotIn(branch, base string) (int, error) {
	out, err := r.run("rev-list", "--count", "--no-merges", r.ref(branch), "--not", r.ref(base))
	if err != nil {
		return 0, err
	}
	return atoiSafe(out), nil
}

func (r *execRepository) ResolveCommit(ref string) (string, error) {
	return r.run("rev-parse", "--verify", "--quiet", r.ref(ref)+"^{commit}")
}

// Tags lit tous les tags en une seule commande ; pour un tag annoté,
// %(*objectname) donne le commit pointé (et %(objectname) l'objet tag).
func (r *execRepository) Tags() (map[string][]string, error) {
	out, err := r.run("for-each-ref", "--format=%(refname:short)|%(objectname)|%(*objectname)", "refs/tags")
	if err != nil {
		return nil, err
	}
	tags := make(map[string][]string)
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			continue
		}
		commit := parts[1]
		if parts[2] != "" {
			commit = parts[2]
		}
		tags[commit] = append(tags[commit], parts[0])
	}
	return tags, nil
}

func (r *execRepository) GitflowConfig() (map[string]string, error) {
	values := make(map[string]string)
	cmd := exec.Command("git", "config", "--get-regexp", `^gitflow\.`)
	cmd.Dir = r.dir
	out, err := cmd.Output()
	if err != nil {
		// Code 1 : aucune clé ne correspond, git flow init jamais lancé.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return values, nil
		}
		return nil, fmt.Errorf("git config --get-regexp gitflow: %w", err)
	}
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		key, value, _ := strings.Cut(line, " ")
		values[key] = value
	}
	return values, nil
}

// IsAncestor renvoie true si ancestor est un ancêtre de descendant (ou lui
// est identique), c'est-à-dire si descendant contient déjà tout le travail
// d'ancestor — le signe qu'une branche a été fusionnée.
func (r *execRepository) IsAncestor(ancestor, descendant string) (bool, error) {
	cmd := exec.Command("git", "merge-base", "--is-ancestor", r.ref(ancestor), r.ref(descendant))
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

// StateFingerprint lit HEAD, packed-refs et les arborescences de refs
// directement sur disque (aucun processus git lancé) et en combine une
// empreinte : n'importe quel commit, fusion, changement de branche, ou
// création/suppression de branche modifie l'un de ces fichiers et fait donc
// varier la valeur renvoyée. HEAD est propre au worktree, les refs sont
// partagées (commonDir) ; le répertoire reftable couvre les dépôts qui
// utilisent ce format de stockage des refs (git 2.45+) à la place de
// refs/heads et packed-refs.
func (r *execRepository) StateFingerprint() string {
	h := fnv.New64a()

	if head, err := os.ReadFile(filepath.Join(r.gitDir, "HEAD")); err == nil {
		h.Write(head)
	}

	if info, err := os.Stat(filepath.Join(r.commonDir, "packed-refs")); err == nil {
		fmt.Fprintf(h, "packed-refs:%d:%d", info.Size(), info.ModTime().UnixNano())
	}

	writeTreeFingerprint(h, filepath.Join(r.commonDir, "refs", "heads"))
	if r.remote != "" {
		writeTreeFingerprint(h, filepath.Join(r.commonDir, "refs", "remotes", r.remote))
	}
	writeTreeFingerprint(h, filepath.Join(r.commonDir, "reftable"))

	return fmt.Sprintf("%x", h.Sum64())
}

// writeTreeFingerprint ajoute à w le chemin relatif, la taille et la date de
// modification de chaque fichier sous root, dans un ordre stable. Un
// répertoire absent n'ajoute rien.
func writeTreeFingerprint(w io.Writer, root string) {
	var entries []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = path
		}
		entries = append(entries, fmt.Sprintf("%s:%d:%d", rel, info.Size(), info.ModTime().UnixNano()))
		return nil
	})
	sort.Strings(entries)
	for _, e := range entries {
		io.WriteString(w, e)
	}
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

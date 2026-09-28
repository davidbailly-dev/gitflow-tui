// Package git est l'infrastructure d'accès aux dépôts : il implémente, en
// lecture seule, le port gitflow.History en s'appuyant sur le binaire git
// (via os/exec) plutôt que de réimplémenter le format git.
package git

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// ErrNotARepo est renvoyée par Open quand le répertoire donné n'est pas (ou
// n'est pas situé dans) un dépôt git.
var ErrNotARepo = errors.New("not a git repository")

// Repository donne accès en lecture seule à un dépôt git.
type Repository struct {
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
func Open(dir, remote string) (*Repository, error) {
	repo := &Repository{dir: dir, remote: remote}
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

// absPath résout un chemin renvoyé par git rev-parse, relatif à la racine
// du dépôt (répertoire depuis lequel git est lancé).
func (r *Repository) absPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(r.dir, path)
}

func (r *Repository) run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// runStatus lance une commande git dont le code de sortie 1 est une réponse
// ("non", "rien trouvé") plutôt qu'une erreur : found vaut alors false.
func (r *Repository) runStatus(args ...string) (out string, found bool, err error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	raw, err := cmd.Output()
	if err == nil {
		return strings.TrimRight(string(raw), "\n"), true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return "", false, nil
	}
	return "", false, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
}

// Root renvoie la racine du working tree.
func (r *Repository) Root() string {
	return r.dir
}

// Remote renvoie le remote dont les branches sont analysées, vide pour les
// branches locales.
func (r *Repository) Remote() string {
	return r.remote
}

// branchRefPrefix est le préfixe des refs des branches analysées.
func (r *Repository) branchRefPrefix() string {
	if r.remote != "" {
		return "refs/remotes/" + r.remote + "/"
	}
	return "refs/heads/"
}

// ref traduit un nom de branche connu en ref complète (ex. "develop" →
// "refs/remotes/origin/develop" en mode remote), pour que git désigne sans
// ambiguïté la branche analysée. Tout autre argument (hash, "hash^2"…) est
// renvoyé tel quel.
func (r *Repository) ref(name string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.branchNames[name] {
		return r.branchRefPrefix() + name
	}
	return name
}

// IsAncestor indique si ancestor est un ancêtre de descendant (ou lui est
// identique), c'est-à-dire si descendant contient déjà tout le travail
// d'ancestor — le signe qu'une branche a été fusionnée.
func (r *Repository) IsAncestor(ancestor, descendant string) (bool, error) {
	_, found, err := r.runStatus("merge-base", "--is-ancestor", r.ref(ancestor), r.ref(descendant))
	return found, err
}

// GitflowConfig renvoie les réglages de la section [gitflow] de la
// configuration git, vide si "git flow init" n'a jamais été lancé.
func (r *Repository) GitflowConfig() (map[string]string, error) {
	values := make(map[string]string)
	out, found, err := r.runStatus("config", "--get-regexp", `^gitflow\.`)
	if err != nil || !found {
		return values, err
	}
	for _, line := range strings.Split(out, "\n") {
		key, value, _ := strings.Cut(line, " ")
		values[key] = value
	}
	return values, nil
}

// Tags lit tous les tags en une seule commande ; pour un tag annoté,
// %(*objectname) donne le commit pointé (et %(objectname) l'objet tag).
func (r *Repository) Tags() (map[string][]string, error) {
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

// Show renvoie le contenu complet d'un commit : message, auteur, date,
// statistiques et diff complet. Les statistiques sont calibrées pour tenir
// dans width colonnes (0 : largeur par défaut de git).
func (r *Repository) Show(hash string, width int) (string, error) {
	stat := "--stat"
	if width > 0 {
		stat = fmt.Sprintf("--stat=%d", width)
	}
	return r.run("show", stat, "--patch", hash)
}

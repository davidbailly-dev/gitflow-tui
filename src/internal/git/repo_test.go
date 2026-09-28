package git

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// gitIn lance git dans dir avec une identité et des dates fixes.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v : %v\n%s", args, err, out)
	}
}

// newRepo crée un dépôt main + develop, avec une feature fusionnée.
func newRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git introuvable")
	}
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "init")
	gitIn(t, dir, "checkout", "-q", "-b", "develop")
	gitIn(t, dir, "checkout", "-q", "-b", "feature/x")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "feat: a | b")
	gitIn(t, dir, "checkout", "-q", "develop")
	gitIn(t, dir, "merge", "-q", "--no-ff", "feature/x", "-m", "Merge branch 'feature/x' into develop")
	return dir
}

func TestLogParsing(t *testing.T) {
	repo, err := Open(newRepo(t), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Branches(); err != nil {
		t.Fatal(err)
	}
	log, err := repo.FirstParentLog("feature/x")
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 2 || log[0].Subject != "feat: a | b" || len(log[0].Parents) != 1 || len(log[0].Hash) != 40 {
		t.Fatalf("ligne directe mal décodée : %+v", log)
	}
	develop, err := repo.FirstParentLog("develop")
	if err != nil {
		t.Fatal(err)
	}
	if len(develop) != 2 || !develop[0].IsMerge() || develop[0].Parents[1] != log[0].Hash {
		t.Fatalf("fusion mal décodée : %+v", develop)
	}
}

func TestGitflowConfigAbsent(t *testing.T) {
	repo, err := Open(newRepo(t), "")
	if err != nil {
		t.Fatal(err)
	}
	values, err := repo.GitflowConfig()
	if err != nil || len(values) != 0 {
		t.Fatalf("config = %v, %v ; attendu vide, sans erreur", values, err)
	}
}

// En mode remote, les noms de branches restent courts et désignent les
// branches du remote, même quand une branche locale du même nom diverge.
func TestRemoteBranches(t *testing.T) {
	origin := newRepo(t)
	clone := filepath.Join(t.TempDir(), "clone")
	gitIn(t, filepath.Dir(clone), "clone", "-q", origin, clone)
	gitIn(t, clone, "checkout", "-q", "develop")
	gitIn(t, clone, "commit", "-q", "--allow-empty", "-m", "local seulement")

	repo, err := Open(clone, "origin")
	if err != nil {
		t.Fatal(err)
	}
	branches, err := repo.Branches()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, b := range branches {
		names[b.Name] = true
	}
	if !names["develop"] || !names["feature/x"] || names["HEAD"] || names["origin/develop"] {
		t.Fatalf("branches du remote = %v", names)
	}
	log, err := repo.FirstParentLog("develop")
	if err != nil {
		t.Fatal(err)
	}
	if log[0].Subject != "Merge branch 'feature/x' into develop" {
		t.Fatalf("develop devrait désigner origin/develop, pointe = %q", log[0].Subject)
	}
}

func TestOpenUnknownRemote(t *testing.T) {
	if _, err := Open(newRepo(t), "nope"); err == nil {
		t.Fatal("un remote inconnu devrait être refusé")
	}
}

// Dans un worktree lié, les refs vivent dans le répertoire .git commun :
// une branche créée doit changer l'empreinte.
func TestFingerprintInWorktree(t *testing.T) {
	dir := newRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitIn(t, dir, "worktree", "add", "-q", wt, "feature/x")

	repo, err := Open(wt, "")
	if err != nil {
		t.Fatal(err)
	}
	before := repo.StateFingerprint()
	gitIn(t, dir, "branch", "feature/y", "develop")
	if repo.StateFingerprint() == before {
		t.Fatal("la création d'une branche n'a pas changé l'empreinte du worktree")
	}
}

// En mode remote, la branche courante est celle du remote que suit la
// branche extraite, pas une branche distante homonyme.
func TestRemoteCurrentBranch(t *testing.T) {
	origin := newRepo(t)
	clone := filepath.Join(t.TempDir(), "clone")
	gitIn(t, filepath.Dir(clone), "clone", "-q", origin, clone)

	current := func() string {
		t.Helper()
		repo, err := Open(clone, "origin")
		if err != nil {
			t.Fatal(err)
		}
		branches, err := repo.Branches()
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range branches {
			if b.IsHead {
				return b.Name
			}
		}
		return ""
	}

	gitIn(t, clone, "checkout", "-q", "develop") // suit origin/develop
	if got := current(); got != "develop" {
		t.Errorf("branche courante = %q, attendu develop", got)
	}
	gitIn(t, clone, "checkout", "-q", "-b", "feature/x", "--no-track") // homonyme, sans suivi
	if got := current(); got != "" {
		t.Errorf("branche courante = %q, attendu aucune (feature/x locale ne suit pas origin)", got)
	}
}

func TestFingerprintCoversTags(t *testing.T) {
	dir := newRepo(t)
	repo, err := Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	before := repo.StateFingerprint()
	gitIn(t, dir, "tag", "v1.0")
	if repo.StateFingerprint() == before {
		t.Fatal("la création d'un tag n'a pas changé l'empreinte")
	}
}

// "|" est autorisé dans un nom de ref : il ne doit pas couper les champs.
func TestRefNamesWithPipe(t *testing.T) {
	dir := newRepo(t)
	gitIn(t, dir, "branch", "feature/a|b")
	gitIn(t, dir, "tag", "v1|rc")
	repo, err := Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	branches, err := repo.Branches()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range branches {
		found = found || (b.Name == "feature/a|b" && b.CommitDate != "")
		if b.Name == "feature/a" {
			t.Error("branche fantôme feature/a")
		}
	}
	if !found {
		t.Errorf("feature/a|b absente ou mal décodée : %+v", branches)
	}
	tags, err := repo.Tags()
	if err != nil {
		t.Fatal(err)
	}
	for commit, names := range tags {
		if len(commit) != 40 {
			t.Errorf("hash mal décodé %q pour %v", commit, names)
		}
	}
}

package git

import (
	"strings"

	"gitflow-tui/internal/gitflow"
)

// commitFormat décrit chaque commit sur une ligne, champs séparés par le
// caractère de contrôle US (\x1f), qui ne peut pas apparaître dans un sujet
// de commit : hash, parents, auteur, date d'auteur (AAAA-MM-JJ), date de
// commit (selon --date), sujet.
const commitFormat = "--pretty=format:%H%x1f%P%x1f%an%x1f%as%x1f%cd%x1f%s"

// dateFormat donne les dates de commit en ISO dans le fuseau local : elles
// se comparent alors comme de simples chaînes.
const dateFormat = "--date=iso-local"

// log lance git log avec args et décode les commits obtenus.
func (r *Repository) log(args ...string) ([]gitflow.Commit, error) {
	out, err := r.run(append([]string{"log", dateFormat, commitFormat}, args...)...)
	if err != nil || out == "" {
		return nil, err
	}
	var commits []gitflow.Commit
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(line, "\x1f", 6)
		if len(f) != 6 {
			continue
		}
		commits = append(commits, gitflow.Commit{
			Hash:       f[0],
			Parents:    strings.Fields(f[1]),
			Author:     f[2],
			AuthorDate: f[3],
			CommitDate: f[4],
			Subject:    f[5],
		})
	}
	return commits, nil
}

// FirstParentLog renvoie la ligne directe de ref (premier parent), du plus
// récent au plus ancien.
func (r *Repository) FirstParentLog(ref string) ([]gitflow.Commit, error) {
	return r.log("--first-parent", r.ref(ref))
}

// CommitsBetween renvoie les commits atteignables depuis tip mais pas depuis
// base : ce qu'une fusion de parents base et tip a apporté.
func (r *Repository) CommitsBetween(base, tip string) ([]gitflow.Commit, error) {
	return r.log(r.ref(tip), "--not", r.ref(base))
}

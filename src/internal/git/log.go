package git

import "strings"

// Commit décrit un commit tel que rapporté par git log.
type Commit struct {
	Hash    string
	Author  string
	Date    string
	Subject string
	IsMerge bool // vrai si le commit a plus d'un parent (commit de fusion)
}

const commitFormat = "%h|%P|%an|%ad|%s"

func parseCommits(out string) []Commit {
	if out == "" {
		return nil
	}
	var commits []Commit
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "|", 5)
		if len(parts) != 5 {
			continue
		}
		hash, parents, author, date, subject := parts[0], parts[1], parts[2], parts[3], parts[4]
		commits = append(commits, Commit{
			Hash:    hash,
			Author:  author,
			Date:    date,
			Subject: subject,
			IsMerge: len(strings.Fields(parents)) > 1,
		})
	}
	return commits
}

// CommitsNotIn renvoie les commits atteignables depuis branch mais pas depuis
// base, c'est-à-dire les commits propres à branch depuis sa divergence.
func (r *execRepository) CommitsNotIn(branch, base string) ([]Commit, error) {
	out, err := r.run("log", branch, "--not", base, "--date=short", "--pretty=format:"+commitFormat)
	if err != nil {
		return nil, err
	}
	return parseCommits(out), nil
}

// MergeCommits renvoie les commits apportés par le commit de fusion
// mergeHash : ceux atteignables depuis son second parent (la branche
// intégrée) mais pas depuis son premier (la branche cible). Fonctionne même
// si la branche source a depuis été supprimée, puisque seul le commit de
// fusion (et ses parents) est nécessaire.
func (r *execRepository) MergeCommits(mergeHash string) ([]Commit, error) {
	parentsOut, err := r.run("log", "-1", "--pretty=format:%P", mergeHash)
	if err != nil {
		return nil, err
	}
	parents := strings.Fields(parentsOut)
	if len(parents) < 2 {
		return nil, nil
	}
	out, err := r.run("log", parents[0]+".."+parents[1], "--date=short", "--pretty=format:"+commitFormat)
	if err != nil {
		return nil, err
	}
	return parseCommits(out), nil
}

// LogSubjects renvoie, pour chaque commit atteignable depuis ref, une ligne
// "hash|date|sujet". Utilisé pour retrouver dans les messages de commit des
// mentions de branches (notamment des fusions), y compris pour des branches
// depuis supprimées.
func (r *execRepository) LogSubjects(ref string) (string, error) {
	return r.run("log", ref, "--date=iso8601", "--pretty=format:%h|%ad|%s")
}

// FirstParentHashes renvoie, du plus récent (ref lui-même) au plus ancien,
// le hash complet de chaque commit atteignable depuis ref en suivant
// uniquement le premier parent — la ligne directe de la branche. Sert à
// déterminer depuis quelle branche principale une autre branche est
// réellement partie, en trouvant le point de correspondance le plus proche
// de sa propre pointe (donc son vrai point de divergence), et non un simple
// ancêtre commun lointain comme le ferait un merge-base une fois la branche
// fusionnée partout.
func (r *execRepository) FirstParentHashes(ref string) ([]string, error) {
	out, err := r.run("log", ref, "--first-parent", "--pretty=format:%H")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// FirstParentCommits renvoie, du plus récent au plus ancien, l'historique
// complet de ref en ne suivant que le premier parent (sa ligne directe) —
// utilisé pour lister les commits/fusions de main et develop, qui n'ont pas
// de branche parente dont soustraire les commits.
func (r *execRepository) FirstParentCommits(ref string) ([]Commit, error) {
	out, err := r.run("log", ref, "--first-parent", "--date=short", "--pretty=format:"+commitFormat)
	if err != nil {
		return nil, err
	}
	return parseCommits(out), nil
}

// Show renvoie le contenu complet d'un commit : message, auteur, date,
// statistiques et diff complet.
func (r *execRepository) Show(hash string) (string, error) {
	return r.run("show", "--stat", "--patch", hash)
}

// DirectCommits renvoie les commits committés directement sur ref (en
// suivant uniquement le premier parent) après sa première fusion connue,
// c'est-à-dire les commits qui ont contourné le flow de fusion attendu une
// fois celui-ci amorcé. Le commit racine et les commits antérieurs à toute
// fusion (phase d'amorçage de la branche : premier commit après sa création)
// sont ignorés, car il n'y a pas d'autre façon de les faire exister.
func (r *execRepository) DirectCommits(ref string) ([]Commit, error) {
	out, err := r.run("log", ref, "--first-parent", "--reverse", "--date=short", "--pretty=format:%h|%P|%an|%ad|%s")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	var commits []Commit
	seenMerge := false
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "|", 5)
		if len(parts) != 5 {
			continue
		}
		hash, parents, author, date, subject := parts[0], parts[1], parts[2], parts[3], parts[4]
		switch len(strings.Fields(parents)) {
		case 0:
			continue // commit racine
		case 1:
			if seenMerge {
				commits = append(commits, Commit{Hash: hash, Author: author, Date: date, Subject: subject})
			}
		default:
			seenMerge = true
		}
	}
	// Repasser du plus récent au plus ancien, comme les autres listes de
	// commits (CommitsNotIn, MergeCommits), pour que la troncature à
	// l'affichage montre les violations les plus récentes en premier.
	for i, j := 0, len(commits)-1; i < j; i, j = i+1, j-1 {
		commits[i], commits[j] = commits[j], commits[i]
	}
	return commits, nil
}

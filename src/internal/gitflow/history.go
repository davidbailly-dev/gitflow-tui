package gitflow

// History est le port de lecture de l'historique d'un dépôt dont l'analyse
// GitFlow a besoin. L'infrastructure (paquet git) l'implémente ; les tests
// le remplacent par un historique en mémoire.
//
// Les références passées en paramètre sont des noms de branches renvoyés
// par Branches, ou des hashes de commits.
type History interface {
	// Branches renvoie les branches à analyser.
	Branches() ([]Branch, error)
	// GitflowConfig renvoie les réglages de la section [gitflow] de la
	// configuration git (clé complète → valeur), vide si "git flow init"
	// n'a jamais été lancé.
	GitflowConfig() (map[string]string, error)
	// Tags renvoie, pour chaque commit étiqueté (hash complet), le nom de
	// ses tags.
	Tags() (map[string][]string, error)
	// FirstParentLog renvoie la ligne directe de ref (en ne suivant que le
	// premier parent), du plus récent au plus ancien.
	FirstParentLog(ref string) ([]Commit, error)
	// CommitsBetween renvoie les commits atteignables depuis tip mais pas
	// depuis base, du plus récent au plus ancien : ce qu'une fusion de
	// premier parent base et de second parent tip a apporté.
	CommitsBetween(base, tip string) ([]Commit, error)
	// AheadBehind compare branch à base : commits propres à branch, et
	// commits de base absents de branch.
	AheadBehind(base, branch string) (ahead, behind int, err error)
	// CountNotIn compte les commits de branch absents de base, commits de
	// fusion exclus.
	CountNotIn(branch, base string) (int, error)
	// IsAncestor indique si ancestor est un ancêtre de descendant (ou lui
	// est identique) : descendant contient déjà tout son travail.
	IsAncestor(ancestor, descendant string) (bool, error)
}

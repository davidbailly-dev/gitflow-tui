package gitflow

import (
	"regexp"
	"strings"
)

// Merge décrit une fusion telle que déduite du sujet de son commit : la
// branche intégrée (Source) et, si le message la précise, la branche qui l'a
// reçue (Target). Target est vide quand git l'omet, c'est-à-dire pour une
// fusion vers la branche principale ou pour un merge de pull request.
type Merge struct {
	Source string
	Target string
}

var (
	// Messages par défaut de git et de GitLab :
	//   Merge branch 'feature/x' into develop
	//   Merge branch 'feature/x' into 'develop'
	//   Merge branch 'develop' of github.com:org/repo into develop
	//   Merge remote-tracking branch 'origin/develop'
	mergeBranchRe = regexp.MustCompile(`^Merge (remote-tracking )?branch '([^']+)'(?: of \S+)?(?: into '?([^'\s]+)'?)?`)
	// GitHub : Merge pull request #12 from org/feature/x
	pullRequestRe = regexp.MustCompile(`^Merge pull request #\d+ from (\S+)`)
	// Bitbucket : Merged in feature/x (pull request #12)
	bitbucketRe = regexp.MustCompile(`^Merged in (\S+)`)
)

// ParseMerge extrait la branche source (et la cible si elle est indiquée)
// du sujet d'un commit de fusion. Seul le début du sujet est interprété :
// une branche simplement citée ailleurs dans le message n'est pas prise
// pour une fusion. ok vaut false si le sujet ne suit aucun format connu.
func ParseMerge(subject string) (m Merge, ok bool) {
	if sm := mergeBranchRe.FindStringSubmatch(subject); sm != nil {
		source := sm[2]
		if sm[1] != "" {
			source = stripFirstSegment(source) // nom du remote
		}
		return Merge{Source: source, Target: sm[3]}, true
	}
	if sm := pullRequestRe.FindStringSubmatch(subject); sm != nil {
		return Merge{Source: stripFirstSegment(sm[1])}, true // propriétaire du fork
	}
	if sm := bitbucketRe.FindStringSubmatch(subject); sm != nil {
		return Merge{Source: sm[1]}, true
	}
	return Merge{}, false
}

// IsPull signale une fusion d'une branche distante dans sa propre copie
// locale (git pull sans rebase) : ce n'est pas une intégration GitFlow.
func (m Merge) IsPull() bool {
	return m.Target != "" && m.Target == m.Source
}

func stripFirstSegment(ref string) string {
	if i := strings.Index(ref, "/"); i >= 0 {
		return ref[i+1:]
	}
	return ref
}

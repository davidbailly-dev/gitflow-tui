package gitflow

// DeviationKind identifie un écart au workflow GitFlow.
type DeviationKind int

const (
	// DeviationUnexpectedMerge : fusion vers une cible non prévue pour ce
	// type de branche (ex. feature fusionnée directement dans main).
	DeviationUnexpectedMerge DeviationKind = iota
	// DeviationPartialMerge : certaines cibles atteintes, pas toutes (ex.
	// hotfix jamais refusionné dans develop).
	DeviationPartialMerge
	// DeviationWrongParent : branche partie de l'autre branche permanente.
	DeviationWrongParent
	// DeviationDirectSync : develop fusionnée directement dans main, sans
	// passer par une release.
	DeviationDirectSync
	// DeviationUntagged : release ou hotfix fusionné dans main sans tag de
	// version.
	DeviationUntagged
	// DeviationStale : branche pas encore fusionnée, sans commit depuis
	// trop longtemps.
	DeviationStale
)

// Deviation est un écart au workflow GitFlow constaté sur une ligne.
type Deviation struct {
	Kind DeviationKind
	// Branch est la branche en cause : cible inattendue, parent réel,
	// branche principale sans tag.
	Branch string
	Date   string // date de la fusion inattendue
	Days   int    // jours d'inactivité
}

// Deviations renvoie les écarts au workflow GitFlow de la ligne, dans un
// ordre stable.
func (l Lane) Deviations() []Deviation {
	var out []Deviation
	if l.Kind == LaneSync {
		out = append(out, Deviation{Kind: DeviationDirectSync})
	}
	if l.Status() == StatusPartial {
		out = append(out, Deviation{Kind: DeviationPartialMerge})
	}
	for _, mr := range l.Unexpected {
		out = append(out, Deviation{Kind: DeviationUnexpectedMerge, Branch: mr.Target, Date: mr.Date})
	}
	if l.UntaggedIn != "" {
		out = append(out, Deviation{Kind: DeviationUntagged, Branch: l.UntaggedIn})
	}
	if l.StaleDays > 0 {
		out = append(out, Deviation{Kind: DeviationStale, Days: l.StaleDays})
	}
	if l.WrongParent != "" {
		out = append(out, Deviation{Kind: DeviationWrongParent, Branch: l.WrongParent})
	}
	return out
}

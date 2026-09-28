package gitflow

import (
	"reflect"
	"testing"
	"time"
)

// newGitflowHistory démarre un dépôt GitFlow : un commit initial sur main,
// et develop créée depuis main (extraite).
func newGitflowHistory(t *testing.T) *fakeHistory {
	h := newFakeHistory(t, "main")
	h.commit("init")
	h.checkoutNew("develop")
	return h
}

// finishFeature crée name depuis develop, y commite subjects, la fusionne
// dans develop et revient sur develop.
func finishFeature(h *fakeHistory, name string, subjects ...string) {
	h.checkout("develop").checkoutNew(name)
	for _, s := range subjects {
		h.commit(s)
	}
	h.checkout("develop").merge(name, "Merge branch '"+name+"' into develop")
}

// finishRelease crée name depuis develop, la fusionne dans main puis dans
// develop, étiquette la fusion dans main avec tag (si non vide) et
// supprime la branche.
func finishRelease(h *fakeHistory, name, tag string) {
	h.checkout("develop").checkoutNew(name)
	h.commit("chore: version de " + name)
	h.checkout("main").merge(name, "Merge branch '"+name+"'")
	if tag != "" {
		h.tag(tag)
	}
	h.checkout("develop").merge(name, "Merge branch '"+name+"' into develop")
	h.deleteBranch(name)
}

func analyze(t *testing.T, h *fakeHistory, opts ...Options) Report {
	t.Helper()
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}
	r, err := Analyze(h, o)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func laneNamed(t *testing.T, r Report, name string) Lane {
	t.Helper()
	for _, l := range r.Lanes {
		if l.Node.Name == name {
			return l
		}
	}
	t.Fatalf("aucune ligne %q ; lignes : %v", name, laneNames(r))
	return Lane{}
}

func laneNames(r Report) []string {
	var names []string
	for _, l := range r.Lanes {
		names = append(names, l.Node.Name)
	}
	return names
}

func branchNamed(t *testing.T, r Report, name string) BranchState {
	t.Helper()
	for _, b := range r.Branches {
		if b.Node.Name == name {
			return b
		}
	}
	t.Fatalf("aucune branche %q", name)
	return BranchState{}
}

func deviationKinds(l Lane) []DeviationKind {
	var kinds []DeviationKind
	for _, d := range l.Deviations() {
		kinds = append(kinds, d.Kind)
	}
	return kinds
}

func subjects(commits []Commit) []string {
	var out []string
	for _, c := range commits {
		out = append(out, c.Subject)
	}
	return out
}

func TestCompliantHistoryHasNoDeviation(t *testing.T) {
	h := newGitflowHistory(t)
	finishFeature(h, "feature/login", "feat: login")
	finishRelease(h, "release/1.0", "v1.0")
	h.deleteBranch("feature/login")

	r := analyze(t, h)
	for _, l := range r.Lanes {
		if devs := l.Deviations(); len(devs) > 0 {
			t.Errorf("%s : écarts inattendus %v", l.Node.Name, devs)
		}
	}
	if len(r.Main.Direct)+len(r.Develop.Direct) > 0 {
		t.Errorf("commits directs inattendus : main %v, develop %v", r.Main.Direct, r.Develop.Direct)
	}
	release := laneNamed(t, r, "release/1.0")
	if release.Kind != LaneDeleted || release.Status() != StatusMerged {
		t.Errorf("release/1.0 : kind %v, statut %v ; attendu supprimée et fusionnée", release.Kind, release.Status())
	}
}

func TestMasterRepository(t *testing.T) {
	h := newFakeHistory(t, "master")
	h.commit("init")
	h.checkoutNew("develop")
	h.checkout("master").checkoutNew("hotfix/1.0.1")
	h.commit("fix: correctif")

	r := analyze(t, h)
	if r.Main == nil || r.Main.Branch.Name != "master" {
		t.Fatalf("branche principale = %+v, attendu master", r.Main)
	}
	hotfix := laneNamed(t, r, "hotfix/1.0.1")
	if want := []string{"master", "develop"}; !reflect.DeepEqual(hotfix.Pending, want) {
		t.Errorf("cibles en attente = %v, attendu %v", hotfix.Pending, want)
	}
	if got := subjects(hotfix.Commits); !reflect.DeepEqual(got, []string{"fix: correctif"}) {
		t.Errorf("commits du hotfix = %v", got)
	}
}

// Une feature fusionnée deux fois dans develop, avec une release entre les
// deux : seule la première fusion a atteint main, via la release. Ce n'est
// pas une fusion directe dans main.
func TestFeatureMergedTwiceIsNotUnexpected(t *testing.T) {
	h := newGitflowHistory(t)
	finishFeature(h, "feature/double", "feat: première partie")
	finishRelease(h, "release/1.0", "")
	h.checkout("feature/double").commit("feat: seconde partie")
	h.checkout("develop").merge("feature/double", "Merge branch 'feature/double' into develop")

	l := laneNamed(t, analyze(t, h), "feature/double")
	if len(l.Unexpected) > 0 {
		t.Errorf("fusions inattendues %v, attendu aucune", l.Unexpected)
	}
}

func TestFeatureMergedIntoMainIsUnexpected(t *testing.T) {
	h := newGitflowHistory(t)
	h.checkoutNew("feature/raccourci")
	h.commit("feat: raccourci")
	h.checkout("main").merge("feature/raccourci", "Merge branch 'feature/raccourci'")
	h.deleteBranch("feature/raccourci")

	l := laneNamed(t, analyze(t, h), "feature/raccourci")
	if got := deviationKinds(l); !reflect.DeepEqual(got, []DeviationKind{DeviationUnexpectedMerge}) {
		t.Errorf("écarts = %v, attendu une fusion inattendue", got)
	}
	if l.Unexpected[0].Target != "main" {
		t.Errorf("cible inattendue = %q, attendu main", l.Unexpected[0].Target)
	}
}

// Une branche seulement citée dans un message de commit ordinaire n'est pas
// une branche fusionnée.
func TestMentionOutsideMergeIsIgnored(t *testing.T) {
	h := newGitflowHistory(t)
	finishFeature(h, "feature/a", "feat: a")
	h.commit("docs: prépare feature/fantome")

	r := analyze(t, h)
	for _, l := range r.Lanes {
		if l.Node.Name == "feature/fantome" {
			t.Fatal("feature/fantome ne devrait pas être reconstituée")
		}
	}
	if got := subjects(r.Develop.Direct); !reflect.DeepEqual(got, []string{"docs: prépare feature/fantome"}) {
		t.Errorf("commits directs = %v", got)
	}
}

func TestDeletedHotfixNotMergedBackIsPartial(t *testing.T) {
	h := newGitflowHistory(t)
	h.checkout("main").checkoutNew("hotfix/1.0.1")
	h.commit("fix: urgent")
	h.checkout("main").merge("hotfix/1.0.1", "Merge branch 'hotfix/1.0.1'")
	h.deleteBranch("hotfix/1.0.1")

	l := laneNamed(t, analyze(t, h), "hotfix/1.0.1")
	if !reflect.DeepEqual(l.Pending, []string{"develop"}) {
		t.Errorf("cibles manquantes = %v, attendu [develop]", l.Pending)
	}
	if got := deviationKinds(l); !reflect.DeepEqual(got, []DeviationKind{DeviationPartialMerge}) {
		t.Errorf("écarts = %v, attendu une fusion partielle", got)
	}
}

// Un hotfix fusionné dans la release en cours rejoint develop avec elle :
// develop n'est pas une cible manquante.
func TestHotfixReachingDevelopThroughRelease(t *testing.T) {
	h := newGitflowHistory(t)
	h.checkoutNew("release/2.0")
	h.commit("chore: version 2.0")
	h.checkout("main").checkoutNew("hotfix/1.0.1")
	h.commit("fix: urgent")
	h.checkout("main").merge("hotfix/1.0.1", "Merge branch 'hotfix/1.0.1'")
	h.checkout("release/2.0").merge("hotfix/1.0.1", "Merge branch 'hotfix/1.0.1' into release/2.0")
	h.deleteBranch("hotfix/1.0.1")
	h.checkout("develop").merge("release/2.0", "Merge branch 'release/2.0' into develop")

	l := laneNamed(t, analyze(t, h), "hotfix/1.0.1")
	if len(l.Pending) > 0 {
		t.Errorf("cibles manquantes = %v, attendu aucune", l.Pending)
	}
}

// Une pull request de develop vers main est une synchronisation directe ;
// une fusion de develop dans une feature, atteignable depuis main ensuite,
// n'en est pas une.
func TestSyncDetection(t *testing.T) {
	h := newGitflowHistory(t)
	h.checkoutNew("feature/sync")
	h.commit("feat: sync")
	h.checkout("develop").commit("feat: autre travail")
	h.checkout("feature/sync").merge("develop", "Merge branch 'develop' into feature/sync")
	h.checkout("develop").merge("feature/sync", "Merge pull request #3 from org/feature/sync")
	h.checkout("main").merge("develop", "Merge pull request #9 from org/develop")

	r := analyze(t, h)
	var syncs []Lane
	for _, l := range r.Lanes {
		if l.Kind == LaneSync {
			syncs = append(syncs, l)
		}
	}
	if len(syncs) != 1 {
		t.Fatalf("%d synchronisations, attendu 1", len(syncs))
	}
	if got := deviationKinds(syncs[0]); !reflect.DeepEqual(got, []DeviationKind{DeviationDirectSync}) {
		t.Errorf("écarts = %v", got)
	}
	if l := laneNamed(t, r, "feature/sync"); l.Status() != StatusMerged {
		t.Errorf("feature/sync : statut %v, attendu fusionnée (merge de pull request)", l.Status())
	}
}

func TestSquashMergeIsNotDirect(t *testing.T) {
	h := newGitflowHistory(t)
	finishFeature(h, "feature/a", "feat: a")
	h.commit("feat: ajoute b (#12)")
	h.commit("wip direct")

	r := analyze(t, h)
	if got := subjects(r.Develop.Direct); !reflect.DeepEqual(got, []string{"wip direct"}) {
		t.Errorf("commits directs = %v, attendu [wip direct]", got)
	}
}

func TestMergedBranchKeepsItsCommits(t *testing.T) {
	h := newGitflowHistory(t)
	finishFeature(h, "feature/gardee", "feat: un", "feat: deux")

	r := analyze(t, h)
	b := branchNamed(t, r, "feature/gardee")
	if got := subjects(b.Commits); !reflect.DeepEqual(got, []string{"feat: deux", "feat: un"}) {
		t.Errorf("commits = %v", got)
	}
	if b.Status != StatusMerged {
		t.Errorf("statut = %v, attendu fusionnée", b.Status)
	}
}

func TestNewEmptyBranchIsNotMerged(t *testing.T) {
	h := newGitflowHistory(t)
	finishFeature(h, "feature/a", "feat: a")
	h.checkoutNew("feature/vide")

	l := laneNamed(t, analyze(t, h), "feature/vide")
	if l.Status() != StatusInProgress || !reflect.DeepEqual(l.Pending, []string{"develop"}) {
		t.Errorf("statut %v, en attente %v ; attendu en cours vers develop", l.Status(), l.Pending)
	}
}

func TestVersionTags(t *testing.T) {
	h := newGitflowHistory(t)
	finishRelease(h, "release/1.0", "v1.0")
	finishRelease(h, "release/1.1", "")

	r := analyze(t, h)
	tagged := laneNamed(t, r, "release/1.0")
	if tagged.UntaggedIn != "" {
		t.Errorf("release/1.0 signalée sans tag")
	}
	for _, mr := range tagged.Merged {
		if mr.Target == "main" && mr.Tag != "v1.0" {
			t.Errorf("tag de la fusion dans main = %q, attendu v1.0", mr.Tag)
		}
	}
	if untagged := laneNamed(t, r, "release/1.1"); untagged.UntaggedIn != "main" {
		t.Errorf("release/1.1 : UntaggedIn = %q, attendu main", untagged.UntaggedIn)
	}
}

// Un dépôt sans aucun tag de version ne suit pas cette convention : pas
// d'écart.
func TestNoTagConventionNoDeviation(t *testing.T) {
	h := newGitflowHistory(t)
	finishRelease(h, "release/1.0", "")

	if l := laneNamed(t, analyze(t, h), "release/1.0"); l.UntaggedIn != "" {
		t.Errorf("UntaggedIn = %q, attendu vide", l.UntaggedIn)
	}
}

func TestStaleBranch(t *testing.T) {
	h := newGitflowHistory(t)
	h.at(time.Date(2026, 3, 1, 9, 0, 0, 0, fakeZone))
	h.checkoutNew("feature/oubliee")
	h.commit("feat: commencée puis oubliée")
	finishFeature(h, "feature/finie", "feat: finie")

	now := time.Date(2026, 5, 1, 9, 0, 0, 0, fakeZone)
	r := analyze(t, h, Options{StaleAfter: 30 * 24 * time.Hour, Now: now})
	if l := laneNamed(t, r, "feature/oubliee"); l.StaleDays != 60 {
		t.Errorf("feature/oubliee : %d jours d'inactivité, attendu 60", l.StaleDays)
	}
	if l := laneNamed(t, r, "feature/finie"); l.StaleDays != 0 {
		t.Errorf("une branche fusionnée n'est pas inactive (%d jours)", l.StaleDays)
	}
}

func TestFeatureStartedFromMainHasWrongParent(t *testing.T) {
	h := newGitflowHistory(t)
	h.checkout("main").checkoutNew("hotfix/1.0.1")
	h.commit("fix: urgent")
	h.checkout("main").merge("hotfix/1.0.1", "Merge branch 'hotfix/1.0.1'")
	h.checkoutNew("feature/mal-partie")
	h.commit("feat: partie de main")

	r := analyze(t, h)
	l := laneNamed(t, r, "feature/mal-partie")
	if l.WrongParent != "main" {
		t.Errorf("WrongParent = %q, attendu main", l.WrongParent)
	}
	// Ses commits propres s'arrêtent là où elle a quitté main : l'historique
	// de main (ici la fusion du hotfix) n'en fait pas partie.
	want := []string{"feat: partie de main"}
	if got := subjects(l.Commits); !reflect.DeepEqual(got, want) {
		t.Errorf("commits de la ligne = %v, attendu %v", got, want)
	}
	if got := subjects(branchNamed(t, r, "feature/mal-partie").Commits); !reflect.DeepEqual(got, want) {
		t.Errorf("commits de la branche = %v, attendu %v", got, want)
	}
}

func TestGitflowConfigConventions(t *testing.T) {
	h := newFakeHistory(t, "production")
	h.config["gitflow.branch.master"] = "production"
	h.config["gitflow.branch.develop"] = "integration"
	h.config["gitflow.prefix.feature"] = "feat/"
	h.commit("init")
	h.checkoutNew("integration")
	h.checkoutNew("feat/x")
	h.commit("feat: x")

	r := analyze(t, h)
	if r.Main == nil || r.Develop == nil || r.Main.Branch.Name != "production" || r.Develop.Branch.Name != "integration" {
		t.Fatalf("branches permanentes mal identifiées : %+v %+v", r.Main, r.Develop)
	}
	if l := laneNamed(t, r, "feat/x"); !reflect.DeepEqual(l.Pending, []string{"integration"}) {
		t.Errorf("feat/x en attente de %v, attendu [integration]", l.Pending)
	}
}

func TestUnreleasedAndDevelopBehind(t *testing.T) {
	h := newGitflowHistory(t)
	finishFeature(h, "feature/a", "feat: a1", "feat: a2")
	h.checkout("main").checkoutNew("hotfix/1.0.1")
	h.commit("fix: urgent")
	h.checkout("main").merge("hotfix/1.0.1", "Merge branch 'hotfix/1.0.1'")

	r := analyze(t, h)
	if r.Unreleased != 2 {
		t.Errorf("Unreleased = %d, attendu 2", r.Unreleased)
	}
	if d := branchNamed(t, r, "develop"); d.Ahead != 2 || d.Behind != 1 {
		t.Errorf("develop ↑%d ↓%d, attendu ↑2 ↓1", d.Ahead, d.Behind)
	}
}

func TestLanesAreChronological(t *testing.T) {
	h := newGitflowHistory(t)
	finishFeature(h, "feature/b", "feat: b")
	finishFeature(h, "feature/a", "feat: a")
	h.deleteBranch("feature/b")

	r := analyze(t, h)
	if got := laneNames(r); !reflect.DeepEqual(got, []string{"feature/b", "feature/a"}) {
		t.Errorf("ordre des lignes = %v, attendu chronologique", got)
	}
}

func TestDirectCommitsIgnoreBootstrap(t *testing.T) {
	h := newGitflowHistory(t)
	h.commit("chore: amorçage de develop")
	finishFeature(h, "feature/a", "feat: a")
	h.commit("fix: direct")

	r := analyze(t, h)
	if got := subjects(r.Develop.Direct); !reflect.DeepEqual(got, []string{"fix: direct"}) {
		t.Errorf("commits directs = %v, attendu [fix: direct]", got)
	}
}

// Sans main ni develop, il n'y a pas de point de divergence : chaque branche
// affiche tout son historique.
func TestNoPermanentBranchShowsFullHistory(t *testing.T) {
	h := newFakeHistory(t, "trunk")
	h.commit("init")
	h.commit("suite")

	b := branchNamed(t, analyze(t, h), "trunk")
	if got := subjects(b.Commits); !reflect.DeepEqual(got, []string{"suite", "init"}) {
		t.Errorf("commits = %v, attendu tout l'historique", got)
	}
}

// Une branche reprise après une première fusion : elle reste placée à sa
// première fusion, et regroupe le travail de ses deux fusions.
func TestBranchMergedTwice(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		h := newGitflowHistory(t)
		finishFeature(h, "feature/reprise", "feat: première partie")
		firstMerge := h.commits[h.branches["develop"]].CommitDate
		finishFeature(h, "feature/autre", "feat: autre")
		h.checkout("feature/reprise").commit("feat: seconde partie")
		h.checkout("develop").merge("feature/reprise", "Merge branch 'feature/reprise' into develop")
		if deleted {
			h.deleteBranch("feature/reprise")
		}

		r := analyze(t, h)
		l := laneNamed(t, r, "feature/reprise")
		if l.Date != firstMerge {
			t.Errorf("supprimée=%v : date %q, attendu la première fusion %q", deleted, l.Date, firstMerge)
		}
		if got, want := laneNames(r), []string{"feature/reprise", "feature/autre"}; !reflect.DeepEqual(got, want) {
			t.Errorf("supprimée=%v : ordre %v, attendu %v", deleted, got, want)
		}
		want := []string{"feat: seconde partie", "feat: première partie"}
		if got := subjects(l.Commits); !reflect.DeepEqual(got, want) {
			t.Errorf("supprimée=%v : commits %v, attendu %v", deleted, got, want)
		}
	}
}

// Une cible de fusion absente des branches analysées n'est pas interrogée
// (en mode remote, git la résoudrait en branche locale homonyme) : elle
// reste en attente.
func TestMissingTargetIsNotQueried(t *testing.T) {
	h := newFakeHistory(t, "main")
	h.commit("init")
	h.checkoutNew("feature/sans-develop")
	h.commit("feat: x")
	// Le faux historique échoue sur toute référence inconnue, dont
	// "develop" si l'analyse l'interrogeait.
	l := laneNamed(t, analyze(t, h), "feature/sans-develop")
	if !reflect.DeepEqual(l.Pending, []string{"develop"}) {
		t.Errorf("en attente = %v, attendu [develop]", l.Pending)
	}
}

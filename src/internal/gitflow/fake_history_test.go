package gitflow

import (
	"crypto/sha1"
	"fmt"
	"sort"
	"strconv"
	"testing"
	"time"
)

// fakeHistory est un dépôt en mémoire qui implémente History : un vrai
// graphe de commits, manipulé comme on le ferait avec git (commit, branche,
// fusion, suppression, tag), pour décrire les scénarios de test en quelques
// lignes.
type fakeHistory struct {
	t        *testing.T
	commits  map[string]Commit
	order    map[string]int    // rang de création : ordre chronologique
	branches map[string]string // nom → hash de la pointe
	head     string            // branche extraite
	tags     map[string][]string
	config   map[string]string
	clock    time.Time
}

// fakeZone fixe le fuseau des dates, pour des comparaisons déterministes.
var fakeZone = time.FixedZone("CEST", 2*3600)

func newFakeHistory(t *testing.T, mainName string) *fakeHistory {
	return &fakeHistory{
		t:        t,
		commits:  make(map[string]Commit),
		order:    make(map[string]int),
		branches: make(map[string]string),
		head:     mainName,
		tags:     make(map[string][]string),
		config:   make(map[string]string),
		clock:    time.Date(2026, 1, 1, 9, 0, 0, 0, fakeZone),
	}
}

// at fixe la date des prochains commits.
func (h *fakeHistory) at(date time.Time) *fakeHistory {
	h.clock = date
	return h
}

func (h *fakeHistory) newCommit(subject string, parents ...string) string {
	h.clock = h.clock.Add(time.Minute)
	seq := len(h.commits)
	hash := fmt.Sprintf("%x", sha1.Sum([]byte(strconv.Itoa(seq))))
	h.commits[hash] = Commit{
		Hash:       hash,
		Parents:    parents,
		Author:     "test",
		AuthorDate: h.clock.Format("2006-01-02"),
		CommitDate: h.clock.Format("2006-01-02 15:04:05 -0700"),
		Subject:    subject,
	}
	h.order[hash] = seq
	return hash
}

// commit ajoute un commit ordinaire sur la branche extraite.
func (h *fakeHistory) commit(subject string) string {
	var parents []string
	if tip, ok := h.branches[h.head]; ok {
		parents = []string{tip}
	}
	hash := h.newCommit(subject, parents...)
	h.branches[h.head] = hash
	return hash
}

// checkout extrait une branche existante.
func (h *fakeHistory) checkout(name string) *fakeHistory {
	if _, ok := h.branches[name]; !ok {
		h.t.Fatalf("checkout : branche %q inconnue", name)
	}
	h.head = name
	return h
}

// checkoutNew crée une branche à la pointe de la branche extraite et
// l'extrait (git checkout -b).
func (h *fakeHistory) checkoutNew(name string) *fakeHistory {
	h.branches[name] = h.branches[h.head]
	h.head = name
	return h
}

// merge fusionne source dans la branche extraite (--no-ff), avec le
// message subject.
func (h *fakeHistory) merge(source, subject string) string {
	hash := h.newCommit(subject, h.branches[h.head], h.mustResolve(source))
	h.branches[h.head] = hash
	return hash
}

func (h *fakeHistory) deleteBranch(name string) {
	delete(h.branches, name)
}

// tag étiquette la pointe de la branche extraite.
func (h *fakeHistory) tag(name string) {
	tip := h.branches[h.head]
	h.tags[tip] = append(h.tags[tip], name)
}

// resolve renvoie le commit désigné par ref (branche ou hash). Une
// référence inconnue est une erreur du scénario ou de l'analyse : elle est
// signalée par t.Errorf, utilisable depuis les goroutines de l'analyse
// (contrairement à t.Fatalf).
func (h *fakeHistory) resolve(ref string) (string, error) {
	if tip, ok := h.branches[ref]; ok {
		return tip, nil
	}
	if _, ok := h.commits[ref]; ok {
		return ref, nil
	}
	h.t.Errorf("référence %q inconnue", ref)
	return "", fmt.Errorf("référence %q inconnue", ref)
}

// mustResolve est resolve pour la construction des scénarios (goroutine du
// test).
func (h *fakeHistory) mustResolve(ref string) string {
	hash, err := h.resolve(ref)
	if err != nil {
		h.t.FailNow()
	}
	return hash
}

// reachable renvoie les commits atteignables depuis ref.
func (h *fakeHistory) reachable(ref string) (map[string]bool, error) {
	start, err := h.resolve(ref)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	stack := []string{start}
	for len(stack) > 0 {
		hash := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if hash == "" || seen[hash] {
			continue
		}
		seen[hash] = true
		stack = append(stack, h.commits[hash].Parents...)
	}
	return seen, nil
}

// reachable2 renvoie les commits atteignables depuis a et depuis b.
func (h *fakeHistory) reachable2(a, b string) (ra, rb map[string]bool, err error) {
	if ra, err = h.reachable(a); err != nil {
		return nil, nil, err
	}
	rb, err = h.reachable(b)
	return ra, rb, err
}

// sorted renvoie les commits de set du plus récent au plus ancien, comme
// git log.
func (h *fakeHistory) sorted(set map[string]bool, keep func(Commit) bool) []Commit {
	var out []Commit
	for hash := range set {
		if c := h.commits[hash]; keep == nil || keep(c) {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return h.order[out[i].Hash] > h.order[out[j].Hash] })
	return out
}

func minus(a, b map[string]bool) map[string]bool {
	out := make(map[string]bool)
	for k := range a {
		if !b[k] {
			out[k] = true
		}
	}
	return out
}

func (h *fakeHistory) Branches() ([]Branch, error) {
	var out []Branch
	for name, tip := range h.branches {
		out = append(out, Branch{Name: name, CommitDate: h.commits[tip].CommitDate, IsHead: name == h.head})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (h *fakeHistory) GitflowConfig() (map[string]string, error) { return h.config, nil }

func (h *fakeHistory) Tags() (map[string][]string, error) { return h.tags, nil }

func (h *fakeHistory) FirstParentLog(ref string) ([]Commit, error) {
	start, err := h.resolve(ref)
	if err != nil {
		return nil, err
	}
	var out []Commit
	for hash := start; hash != ""; {
		c := h.commits[hash]
		out = append(out, c)
		hash = ""
		if len(c.Parents) > 0 {
			hash = c.Parents[0]
		}
	}
	return out, nil
}

func (h *fakeHistory) CommitsBetween(base, tip string) ([]Commit, error) {
	rb, rt, err := h.reachable2(base, tip)
	if err != nil {
		return nil, err
	}
	return h.sorted(minus(rt, rb), nil), nil
}

func (h *fakeHistory) AheadBehind(base, branch string) (int, int, error) {
	b, r, err := h.reachable2(base, branch)
	if err != nil {
		return 0, 0, err
	}
	return len(minus(r, b)), len(minus(b, r)), nil
}

func (h *fakeHistory) CountNotIn(branch, base string) (int, error) {
	r, b, err := h.reachable2(branch, base)
	if err != nil {
		return 0, err
	}
	notMerge := func(c Commit) bool { return !c.IsMerge() }
	return len(h.sorted(minus(r, b), notMerge)), nil
}

func (h *fakeHistory) IsAncestor(ancestor, descendant string) (bool, error) {
	a, err := h.resolve(ancestor)
	if err != nil {
		return false, err
	}
	r, err := h.reachable(descendant)
	if err != nil {
		return false, err
	}
	return r[a], nil
}

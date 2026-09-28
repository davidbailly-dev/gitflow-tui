package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"gitflow-tui/internal/gitflow"
)

func init() {
	// Couleurs forcées, pour vérifier que les styles survivent au découpage.
	lipgloss.SetColorProfile(termenv.ANSI256)
}

func TestCutLeft(t *testing.T) {
	styled := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("abcdef")
	got := cutLeft(styled, 2)
	if ansi.Strip(got) != "cdef" {
		t.Errorf("texte = %q, attendu cdef", ansi.Strip(got))
	}
	if !strings.Contains(got, "\x1b[") {
		t.Error("le style a été perdu")
	}
	if cutLeft("abc", 0) != "abc" || ansi.Strip(cutLeft("abc", 5)) != "" {
		t.Error("bornes mal gérées")
	}
}

// Chaque morceau d'une ligne de diff repliée garde la couleur de la ligne.
func TestRenderDiffWrapsKeepingStyle(t *testing.T) {
	out := renderDiff(nil, "+"+strings.Repeat("x", 25), 10)
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("%d lignes, attendu 3", len(lines))
	}
	for _, l := range lines {
		if ansi.StringWidth(l) > 10 || !strings.Contains(l, "\x1b[") {
			t.Errorf("ligne %q trop large ou sans couleur", l)
		}
	}
}

func TestNewGraphDataModes(t *testing.T) {
	clean := gitflow.Lane{Node: gitflow.Node{Name: "feature/ok"}, Merged: []gitflow.MergeRef{{Target: "develop"}}}
	pending := gitflow.Lane{Node: gitflow.Node{Name: "feature/wip"}, Pending: []string{"develop"}}
	stale := gitflow.Lane{Node: gitflow.Node{Name: "feature/old"}, Pending: []string{"develop"}, StaleDays: 40}
	report := gitflow.Report{
		Develop: &gitflow.Spine{Direct: []gitflow.Commit{{Subject: "direct"}}},
		Lanes:   []gitflow.Lane{clean, pending, stale},
	}

	names := func(g graphData) []string {
		var out []string
		for i, l := range g.lanes {
			if l.col != i {
				t.Errorf("%s : colonne %d, attendu %d (colonnes compactées)", l.Node.Name, l.col, i)
			}
			out = append(out, l.Node.Name)
		}
		return out
	}

	if got := names(newGraphData(report, graphHistory, false)); len(got) != 3 {
		t.Errorf("historique complet : %v", got)
	}
	if got := names(newGraphData(report, graphHistory, true)); len(got) != 1 || got[0] != "feature/old" {
		t.Errorf("écarts seulement : %v, attendu [feature/old]", got)
	}
	live := newGraphData(report, graphLive, false)
	if got := names(live); len(got) != 2 || len(live.developDirect) != 0 {
		t.Errorf("direct : %v (commits directs %d), attendu les 2 branches en cours, sans commit direct", got, len(live.developDirect))
	}
}

// La sélection suit une branche qui est fusionnée, puis supprimée, pendant
// qu'on la regarde ; deux synchronisations restent distinctes.
func TestLaneKeyIsStable(t *testing.T) {
	live := graphLane{Lane: gitflow.Lane{Node: gitflow.Node{Name: "feature/x"}, Kind: gitflow.LaneLive, Date: "2026-05-01 10:00:00 +0200"}}
	merged := live
	merged.Date = "2026-05-03 10:00:00 +0200"
	deleted := merged
	deleted.Kind = gitflow.LaneDeleted
	if laneKey(live) != laneKey(merged) || laneKey(merged) != laneKey(deleted) {
		t.Error("la clé d'une branche change avec son état")
	}

	sync1 := graphLane{Lane: gitflow.Lane{Node: gitflow.Node{Name: "develop → main"}, Kind: gitflow.LaneSync, Date: "2026-05-01 10:00:00 +0200"}}
	sync2 := sync1
	sync2.Date = "2026-06-01 10:00:00 +0200"
	if laneKey(sync1) == laneKey(sync2) {
		t.Error("deux synchronisations ont la même clé")
	}
}

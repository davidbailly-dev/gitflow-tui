package tui

import (
	"os"
	"testing"

	"gitflow-tui/internal/git"
)

// BenchmarkLoadData mesure un chargement complet (vue colonnes et graphe)
// sur le dépôt désigné par GITFLOW_TUI_BENCH_REPO ; ignoré sinon.
func BenchmarkLoadData(b *testing.B) {
	dir := os.Getenv("GITFLOW_TUI_BENCH_REPO")
	if dir == "" {
		b.Skip("GITFLOW_TUI_BENCH_REPO non défini")
	}
	repo, err := git.Open(dir, "")
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		if msg := loadData(repo, Options{})().(dataLoadedMsg); msg.err != nil {
			b.Fatal(msg.err)
		}
	}
}

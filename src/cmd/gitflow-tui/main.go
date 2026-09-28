package main

import (
	"flag"
	"fmt"
	"os"
	"runtime/debug"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"gitflow-tui/internal/git"
	"gitflow-tui/internal/tui"
)

// version est injectée au build (-ldflags "-X main.version=1.2.0") ; vide,
// elle est déduite des informations de build de Go (cf. buildVersion).
var version string

// buildVersion renvoie la version affichée par --version : celle injectée
// au build, sinon celle du module (go install …@v1.2.0), sinon la révision
// git compilée, suffixée de "-modifié" si l'arbre de travail n'était pas
// propre.
func buildVersion() string {
	if version != "" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	var revision, modified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				modified = "-modifié"
			}
		}
	}
	if len(revision) > 7 {
		revision = revision[:7]
	}
	if revision == "" {
		return "dev"
	}
	return "dev (" + revision + modified + ")"
}

func main() {
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "Usage : gitflow-tui [options] [chemin du dépôt]")
		fmt.Fprintln(flag.CommandLine.Output())
		flag.PrintDefaults()
	}
	showVersion := flag.Bool("version", false, "affiche la version et quitte")
	flag.BoolVar(showVersion, "v", false, "affiche la version et quitte (raccourci)")
	remote := flag.String("remote", "", "analyse les branches de ce remote (ex. origin) au lieu des branches locales")
	staleDays := flag.Int("stale-days", 30, "signale les branches sans commit depuis ce nombre de jours (0 : jamais)")
	flag.Parse()

	if *showVersion {
		fmt.Println("gitflow-tui version", buildVersion())
		return
	}

	dir := flag.Arg(0)
	if dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, "gitflow-tui: impossible de déterminer le répertoire courant:", err)
			os.Exit(1)
		}
		dir = cwd
	}

	repo, err := git.Open(dir, *remote)
	switch {
	case err == git.ErrNotARepo:
		fmt.Fprintf(os.Stderr, "gitflow-tui: %s n'est pas un dépôt git.\n", dir)
		os.Exit(1)
	case err != nil:
		fmt.Fprintln(os.Stderr, "gitflow-tui:", err)
		os.Exit(1)
	}

	opts := tui.Options{StaleAfter: time.Duration(*staleDays) * 24 * time.Hour}
	p := tea.NewProgram(tui.New(repo, opts), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "gitflow-tui:", err)
		os.Exit(1)
	}
}

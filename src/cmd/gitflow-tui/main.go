package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"gitflow-tui/internal/git"
	"gitflow-tui/internal/tui"
)

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "gitflow-tui: impossible de déterminer le répertoire courant:", err)
		os.Exit(1)
	}

	repo, err := git.Open(cwd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gitflow-tui: ce répertoire n'est pas un dépôt git.")
		os.Exit(1)
	}

	p := tea.NewProgram(tui.New(repo), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "gitflow-tui:", err)
		os.Exit(1)
	}
}

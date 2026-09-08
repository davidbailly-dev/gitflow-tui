package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Up      key.Binding
	Down    key.Binding
	Left    key.Binding
	Right   key.Binding
	Tab     key.Binding
	Mode    key.Binding
	Refresh key.Binding
	Filter  key.Binding
	Help    key.Binding
	Quit    key.Binding
}

var keys = keyMap{
	Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "monter")),
	Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "descendre")),
	Left:    key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "colonne préc.")),
	Right:   key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "colonne suiv.")),
	Tab:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "changer de vue")),
	Mode:    key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "historique/direct")),
	Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rafraîchir")),
	Filter:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filtrer")),
	Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "aide")),
	Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quitter")),
}

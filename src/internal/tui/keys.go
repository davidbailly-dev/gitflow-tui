package tui

import "github.com/charmbracelet/bubbles/key"

// keyMap regroupe les raccourcis clavier. Leur texte d'aide (WithHelp) est
// l'unique source du pied de page et de l'écran d'aide.
type keyMap struct {
	Up      key.Binding
	Down    key.Binding
	Left    key.Binding
	Right   key.Binding
	Tab     key.Binding
	Mode    key.Binding
	Alerts  key.Binding
	Refresh key.Binding
	Filter  key.Binding
	Help    key.Binding
	Quit    key.Binding

	// Navigate et Scroll ne servent qu'à l'affichage : ils résument dans le
	// pied de page les touches de déplacement, testées individuellement.
	Navigate key.Binding
	Scroll   key.Binding
}

var keys = keyMap{
	Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "monter")),
	Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "descendre")),
	Left:    key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "panneau précédent")),
	Right:   key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "panneau suivant")),
	Tab:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "changer de vue")),
	Mode:    key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "historique/direct")),
	Alerts:  key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "écarts/complet")),
	Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rafraîchir")),
	Filter:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filtrer")),
	Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "aide")),
	Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quitter")),

	Navigate: key.NewBinding(key.WithHelp("↑↓←→/hjkl", "naviguer")),
	Scroll:   key.NewBinding(key.WithHelp("↑↓←→", "défiler")),
}

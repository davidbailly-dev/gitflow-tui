package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"gitflow-tui/internal/git"
)

// tabWidth est le nombre d'espaces qui remplacent une tabulation du diff :
// une tabulation brute n'a pas de largeur fixe à l'écran, ce qui fausserait
// le calcul du retour à la ligne.
const tabWidth = 4

// renderDiff met en forme le contenu d'un commit pour le panneau "contenu"
// de largeur width : la liste des commits apportés s'il s'agit d'une
// fusion, puis la sortie de git show, colorée ligne par ligne (en-tête du
// commit, fichiers, blocs, ajouts, suppressions).
//
// Le retour à la ligne est fait ici, sur le texte brut, avant coloration :
// chaque morceau d'une ligne repliée reçoit ainsi la couleur de sa ligne
// d'origine, alors que le retour à la ligne automatique du viewport la
// perdrait sur la partie repliée.
func renderDiff(merged []git.Commit, show string, width int) string {
	width = maxInt(width, 1)
	var out []string

	if len(merged) > 0 {
		header := lipgloss.NewStyle().Foreground(colorMerge).Bold(true)
		out = append(out, header.Render(ansi.Truncate(fmt.Sprintf("⑂ %d commit(s) apporté(s) par cette fusion :", len(merged)), width, "…")))
		for _, c := range merged {
			out = append(out, ansi.Truncate(fmt.Sprintf("· %s %s", c.Hash, c.Subject), width, "…"))
		}
		out = append(out, "")
	}

	for _, line := range strings.Split(show, "\n") {
		line = strings.ReplaceAll(line, "\t", strings.Repeat(" ", tabWidth))
		style := diffLineStyle(line)
		for _, chunk := range strings.Split(ansi.Hardwrap(line, width, true), "\n") {
			out = append(out, style.Render(chunk))
		}
	}
	return strings.Join(out, "\n")
}

// diffLineStyle choisit le style d'une ligne de git show d'après son
// préfixe. Les lignes du message de commit sont indentées par git et ne
// peuvent donc pas être confondues avec des lignes du diff.
func diffLineStyle(line string) lipgloss.Style {
	base := lipgloss.NewStyle()
	switch {
	case strings.HasPrefix(line, "commit "):
		return base.Foreground(colorDiffHash).Bold(true)
	case strings.HasPrefix(line, "diff --git"),
		strings.HasPrefix(line, "+++ "),
		strings.HasPrefix(line, "--- "):
		return base.Bold(true)
	case strings.HasPrefix(line, "index "),
		strings.HasPrefix(line, "new file"),
		strings.HasPrefix(line, "deleted file"),
		strings.HasPrefix(line, "similarity index"),
		strings.HasPrefix(line, "rename "):
		return base.Faint(true)
	case strings.HasPrefix(line, "@@"):
		return base.Foreground(colorDiffHunk)
	case strings.HasPrefix(line, "+"):
		return base.Foreground(colorDiffAdd)
	case strings.HasPrefix(line, "-"):
		return base.Foreground(colorDiffDel)
	}
	return base
}

// cutLeft retire les n premières colonnes affichées de s, en conservant
// toutes les séquences d'échappement ANSI rencontrées (couleurs) pour que la
// partie restante garde son style. Sert au défilement horizontal.
func cutLeft(s string, n int) string {
	if n <= 0 {
		return s
	}
	var b strings.Builder
	skipped := 0
	for i := 0; i < len(s); {
		if s[i] == '\x1b' {
			j := i + 1
			if j < len(s) && s[j] == '[' {
				// CSI : paramètres puis un octet final entre 0x40 et 0x7e.
				j++
				for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
					j++
				}
			}
			j = minInt(j+1, len(s))
			b.WriteString(s[i:j])
			i = j
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if skipped < n {
			skipped += ansi.StringWidth(string(r))
		} else {
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

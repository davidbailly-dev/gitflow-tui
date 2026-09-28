package gitflow

import "strings"

// Config décrit les conventions de nommage GitFlow d'un dépôt : noms des
// branches permanentes, préfixes des branches de travail et préfixe des
// tags de version. Elle reprend les réglages enregistrés par
// "git flow init" (section [gitflow] de la configuration git).
type Config struct {
	// Main et Develop sont vides pour une détection automatique (main, ou
	// master à défaut ; develop).
	Main    string
	Develop string

	FeaturePrefix string
	BugfixPrefix  string
	ReleasePrefix string
	HotfixPrefix  string
	SupportPrefix string

	// VersionTagPrefix restreint les tags considérés comme des tags de
	// version (ex. "v") ; vide, tous les tags le sont.
	VersionTagPrefix string
}

// DefaultConfig renvoie les conventions GitFlow habituelles.
func DefaultConfig() Config {
	return Config{
		FeaturePrefix: "feature/",
		BugfixPrefix:  "bugfix/",
		ReleasePrefix: "release/",
		HotfixPrefix:  "hotfix/",
		SupportPrefix: "support/",
	}
}

// ConfigFromGit construit la configuration à partir des valeurs de la
// section [gitflow] de la configuration git (clé complète → valeur, ex.
// "gitflow.prefix.feature" → "feat/"). Les réglages absents gardent leur
// valeur par défaut.
func ConfigFromGit(values map[string]string) Config {
	c := DefaultConfig()
	set := func(dst *string, key string) {
		if v, ok := values[key]; ok && strings.TrimSpace(v) != "" {
			*dst = strings.TrimSpace(v)
		}
	}
	set(&c.Main, "gitflow.branch.master")
	set(&c.Main, "gitflow.branch.main")
	set(&c.Develop, "gitflow.branch.develop")
	set(&c.FeaturePrefix, "gitflow.prefix.feature")
	set(&c.BugfixPrefix, "gitflow.prefix.bugfix")
	set(&c.ReleasePrefix, "gitflow.prefix.release")
	set(&c.HotfixPrefix, "gitflow.prefix.hotfix")
	set(&c.SupportPrefix, "gitflow.prefix.support")
	if v, ok := values["gitflow.prefix.versiontag"]; ok {
		c.VersionTagPrefix = strings.TrimSpace(v)
	}
	return c
}

// IsVersionTag indique si tag est un tag de version selon la convention du
// dépôt.
func (c Config) IsVersionTag(tag string) bool {
	return strings.HasPrefix(tag, c.VersionTagPrefix)
}

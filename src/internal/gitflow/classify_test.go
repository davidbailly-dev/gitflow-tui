package gitflow

import (
	"reflect"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name       string
		wantType   BranchType
		wantParent string
	}{
		{"main", TypeMain, ""},
		{"master", TypeMain, ""},
		{"develop", TypeDevelop, ""},
		{"feature/login", TypeFeature, "develop"},
		{"release/1.2.0", TypeRelease, "develop"},
		{"hotfix/1.2.1", TypeHotfix, "main"},
		{"bugfix/crash", TypeBugfix, "develop"},
		{"support/1.x", TypeSupport, "main"},
		{"chore/cleanup", TypeOther, ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			nodes := Classify([]string{c.name})
			if len(nodes) != 1 {
				t.Fatalf("attendu 1 noeud, obtenu %d", len(nodes))
			}
			got := nodes[0]
			if got.Type != c.wantType {
				t.Errorf("type = %v, attendu %v", got.Type, c.wantType)
			}
			if got.Parent != c.wantParent {
				t.Errorf("parent = %q, attendu %q", got.Parent, c.wantParent)
			}
		})
	}
}

func TestClassifierMaster(t *testing.T) {
	c := NewClassifier([]string{"master", "develop", "hotfix/1.0.1"}, DefaultConfig())
	if c.Main != "master" {
		t.Fatalf("Main = %q, attendu master", c.Main)
	}

	hotfix := c.ClassifyOne("hotfix/1.0.1")
	if hotfix.Parent != "master" {
		t.Errorf("parent du hotfix = %q, attendu master", hotfix.Parent)
	}
	if want := []string{"master", "develop"}; !reflect.DeepEqual(hotfix.MergeTargets, want) {
		t.Errorf("cibles du hotfix = %v, attendu %v", hotfix.MergeTargets, want)
	}

	release := c.ClassifyOne("release/1.0.0")
	if want := []string{"develop", "master"}; !reflect.DeepEqual(release.MergeTargets, want) {
		t.Errorf("cibles de la release = %v, attendu %v", release.MergeTargets, want)
	}
}

func TestClassifierMainAndMaster(t *testing.T) {
	c := NewClassifier([]string{"master", "main", "develop"}, DefaultConfig())
	if c.Main != "main" {
		t.Fatalf("Main = %q, attendu main", c.Main)
	}
	if got := c.ClassifyOne("master").Type; got != TypeOther {
		t.Errorf("type de master = %v, attendu TypeOther", got)
	}
}

func TestClassifierGitflowConfig(t *testing.T) {
	cfg := ConfigFromGit(map[string]string{
		"gitflow.branch.master":  "production",
		"gitflow.branch.develop": "integration",
		"gitflow.prefix.feature": "feat/",
		"gitflow.prefix.hotfix":  "fix/",
	})
	c := NewClassifier([]string{"production", "integration", "main"}, cfg)

	cases := []struct {
		name       string
		wantType   BranchType
		wantParent string
	}{
		{"production", TypeMain, ""},
		{"integration", TypeDevelop, ""},
		{"main", TypeOther, ""},
		{"feat/login", TypeFeature, "integration"},
		{"feature/login", TypeOther, ""},
		{"fix/1.0.1", TypeHotfix, "production"},
		{"release/1.0", TypeRelease, "integration"},
	}
	for _, cs := range cases {
		got := c.ClassifyOne(cs.name)
		if got.Type != cs.wantType || got.Parent != cs.wantParent {
			t.Errorf("%s : type %v, parent %q ; attendu %v, %q", cs.name, got.Type, got.Parent, cs.wantType, cs.wantParent)
		}
	}
}

package gitflow

import "testing"

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

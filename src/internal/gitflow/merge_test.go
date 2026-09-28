package gitflow

import "testing"

func TestParseMerge(t *testing.T) {
	cases := []struct {
		subject string
		want    Merge
		wantOK  bool
	}{
		{"Merge branch 'feature/login' into develop", Merge{Source: "feature/login", Target: "develop"}, true},
		{"Merge branch 'release/1.0'", Merge{Source: "release/1.0"}, true},
		{"Merge branch 'feature/x' into 'develop'", Merge{Source: "feature/x", Target: "develop"}, true},
		{"Merge branch 'develop' into feature/x", Merge{Source: "develop", Target: "feature/x"}, true},
		{"Merge branch 'develop' of github.com:org/repo into develop", Merge{Source: "develop", Target: "develop"}, true},
		{"Merge remote-tracking branch 'origin/develop'", Merge{Source: "develop"}, true},
		{"Merge remote-tracking branch 'origin/feature/x' into develop", Merge{Source: "feature/x", Target: "develop"}, true},
		{"Merge pull request #5 from davidbailly-dev/feature/rafraichissement-automatique", Merge{Source: "feature/rafraichissement-automatique"}, true},
		{"Merge pull request #6 from org/develop", Merge{Source: "develop"}, true},
		{"Merged in feature/x (pull request #12)", Merge{Source: "feature/x"}, true},
		{"docs: prépare feature/fantome", Merge{}, false},
		{"chore: mention de Merge branch 'feature/x'", Merge{}, false},
	}

	for _, c := range cases {
		t.Run(c.subject, func(t *testing.T) {
			got, ok := ParseMerge(c.subject)
			if ok != c.wantOK || got != c.want {
				t.Errorf("ParseMerge = %+v, %v ; attendu %+v, %v", got, ok, c.want, c.wantOK)
			}
		})
	}
}

func TestMergeIsPull(t *testing.T) {
	pull, _ := ParseMerge("Merge branch 'develop' of github.com:org/repo into develop")
	if !pull.IsPull() {
		t.Error("un git pull devrait être reconnu comme tel")
	}
	merge, _ := ParseMerge("Merge branch 'feature/x' into develop")
	if merge.IsPull() {
		t.Error("une fusion de feature ne devrait pas être prise pour un git pull")
	}
}

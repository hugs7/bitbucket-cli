package pr

import "testing"

func TestBranchToTitle(t *testing.T) {
	tests := map[string]string{
		"feature/some-feature":   "Feature: Some feature",
		"bugfix/fix_the_thing":   "Bugfix: Fix the thing",
		"hotfix/JIRA-123-broken": "Hotfix: JIRA 123 broken",
		"some-feature":           "Some feature",
		"  chore/update_docs  ":  "Chore: Update docs",
		"":                       "",
	}

	for branch, want := range tests {
		if got := BranchToTitle(branch); got != want {
			t.Fatalf("BranchToTitle(%q) = %q, want %q", branch, got, want)
		}
	}
}

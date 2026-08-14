package gitctx

import "testing"

func TestCanonicalBranchName(t *testing.T) {
	known := []string{"master", "Release-Nov26", "bugfix/splunk"}
	tests := map[string]string{
		" release-Nov26 ": "Release-Nov26",
		"BUGFIX/SPLUNK":   "bugfix/splunk",
		"feature/new":     "feature/new",
	}

	for branch, want := range tests {
		if got := CanonicalBranchName(branch, known); got != want {
			t.Fatalf("CanonicalBranchName(%q) = %q, want %q", branch, got, want)
		}
	}
}

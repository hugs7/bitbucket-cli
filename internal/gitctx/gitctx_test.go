package gitctx

import (
	"os/exec"
	"testing"
)

func TestAtPathPrefersBitbucket(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", dir},
		{"-C", dir, "remote", "add", "origin", "git@github.com:someone/repo.git"},
		{"-C", dir, "remote", "add", "work", "ssh://git@bitbucket.example.com/PROJ/repo.git"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	for _, tc := range []struct{ remote, want string }{{"", "work"}, {"origin", "origin"}, {"work", "work"}} {
		r, err := AtPath(dir, tc.remote)
		if err != nil || r.RemoteName != tc.want {
			t.Fatalf("AtPath(%q) = %v, %v; want %s", tc.remote, r, err, tc.want)
		}
	}
}

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

package pr

import (
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPushUsesSelectedRemote(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "--bare", filepath.Join(dir, "other.git"))
	git("init", "--bare", filepath.Join(dir, "work.git"))
	git("init", "local")
	t.Chdir(filepath.Join(dir, "local"))
	git("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "initial")
	git("branch", "feature/work")
	git("remote", "add", "origin", filepath.Join(dir, "other.git"))
	git("remote", "add", "bitbucket", filepath.Join(dir, "work.git"))
	if err := gitPushBranch("bitbucket", "feature/work", io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if got := git("ls-remote", "--heads", "origin"); got != "" {
		t.Fatalf("wrote wrong remote: %s", got)
	}
	if got := git("ls-remote", "--heads", "bitbucket"); !strings.Contains(got, "refs/heads/feature/work") {
		t.Fatalf("missing branch: %s", got)
	}
	if got := git("rev-parse", "--abbrev-ref", "feature/work@{upstream}"); got != "bitbucket/feature/work" {
		t.Fatalf("upstream = %q, want bitbucket/feature/work", got)
	}

	// An upstream already pointing at another remote is kept.
	git("branch", "feature/mirrored")
	git("push", "-u", "origin", "feature/mirrored")
	if err := gitPushBranch("bitbucket", "feature/mirrored", io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if got := git("rev-parse", "--abbrev-ref", "feature/mirrored@{upstream}"); got != "origin/feature/mirrored" {
		t.Fatalf("upstream = %q, want origin/feature/mirrored", got)
	}
}

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

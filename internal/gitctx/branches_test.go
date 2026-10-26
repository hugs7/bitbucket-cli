package gitctx

import (
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestTargetBranches(t *testing.T) {
	dir := t.TempDir()
	git := func(date string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_COMMITTER_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_EMAIL=test@example.com", "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s (%v)", args, out, err)
		}
		return strings.TrimSpace(string(out))
	}
	date := "2026-01-01T00:00:00Z"
	git(date, "init")
	tree := git(date, "mktree")
	root := git(date, "commit-tree", tree, "-m", "root")
	base := git("2026-01-03T00:00:00Z", "commit-tree", tree, "-p", root, "-m", "release")
	source := git("2026-01-04T00:00:00Z", "commit-tree", tree, "-p", base, "-m", "feature")
	newerBase := git("2026-01-05T00:00:00Z", "commit-tree", tree, "-p", base, "-m", "advanced release")
	oldAncestorNewTip := git("2026-01-09T00:00:00Z", "commit-tree", tree, "-p", root, "-m", "new main")
	orphan := git("2026-02-01T00:00:00Z", "commit-tree", tree, "-m", "unrelated")
	for name, hash := range map[string]string{
		"refs/heads/feature/work":               source,
		"refs/heads/release/current":            root, // stale local must lose to selected remote
		"refs/remotes/work/release/current":     newerBase,
		"refs/remotes/work/release/older":       base,
		"refs/remotes/work/main":                oldAncestorNewTip,
		"refs/remotes/work/z-unrelated":         orphan,
		"refs/remotes/work/a-unrelated":         orphan,
		"refs/remotes/github/should-not-appear": source,
	} {
		git(date, "update-ref", name, hash)
	}
	git(date, "symbolic-ref", "refs/remotes/work/HEAD", "refs/remotes/work/main")
	got, inferred := TargetBranches(dir, "work", "feature/work")
	want := []string{"release/current", "release/older", "main", "a-unrelated", "z-unrelated"}
	if !reflect.DeepEqual(got, want) || inferred != "release/current" {
		t.Fatalf("got %v (%s), want %v (release/current)", got, inferred, want)
	}
	got, inferred = TargetBranches(dir, "work", "missing")
	if inferred != "" || got[0] != "a-unrelated" {
		t.Fatalf("missing source should fall back alphabetically: %v %s", got, inferred)
	}
	git(date, "update-ref", "refs/heads/isolated", git(date, "commit-tree", tree, "-m", "isolated"))
	got, inferred = TargetBranches(dir, "work", "isolated")
	if inferred != "" || got[0] != "a-unrelated" {
		t.Fatalf("no shared ancestor should fall back alphabetically: %v %s", got, inferred)
	}
}

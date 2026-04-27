package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDetectPackageManager_ManualInstallInBrewPrefix locks in the
// behaviour the user relies on: a regular-file `bb` copied into
// /opt/homebrew/bin (or any other brew-prefix bin/) is *not*
// detected as a brew install — only files that symlink into
// .../Cellar/... are. Otherwise plain manual installs that just
// happen to share a directory with brew binaries would refuse to
// self-update.
func TestDetectPackageManager_ManualInstallInBrewPrefix(t *testing.T) {
	dir := t.TempDir()
	// Pretend this is /opt/homebrew/bin/bb but as a regular file
	// (a manual `cp` install, not a brew-managed symlink).
	bin := filepath.Join(dir, "bb")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := detectPackageManager(bin); got != nil {
		t.Errorf("manual install was detected as %q — expected nil", got.name)
	}
}

// TestDetectPackageManager_BrewSymlink simulates a real Homebrew
// install: bin/bb → Cellar/bitbucket-cli/<v>/bin/bb. EvalSymlinks
// has to resolve the link before the /Cellar/ substring check fires,
// so this guards against regressions where we forget to resolve.
func TestDetectPackageManager_BrewSymlink(t *testing.T) {
	dir := t.TempDir()
	cellar := filepath.Join(dir, "Cellar", "bitbucket-cli", "0.3.0", "bin")
	if err := os.MkdirAll(cellar, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(cellar, "bb")
	if err := os.WriteFile(target, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(binDir, "bb")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	pm := detectPackageManager(link)
	if pm == nil {
		t.Fatalf("brew-style symlink was not detected as a package install")
	}
	if pm.name != "Homebrew" {
		t.Errorf("expected Homebrew, got %q", pm.name)
	}
}

func TestDetectPackageManager_EmptyPath(t *testing.T) {
	if got := detectPackageManager(""); got != nil {
		t.Errorf("empty path returned %q — expected nil", got.name)
	}
}

// TestIsNewerVersion locks in the basic semver comparison the update
// notifier relies on. Edge cases that have bitten string-compare
// implementations in the past — the 9→10 minor jump, mismatched
// "v" prefixes, and pre-release suffixes — get explicit coverage.
func TestIsNewerVersion(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"0.3.1", "0.3.0", true},
		{"0.3.0", "0.3.0", false},
		{"0.3.0", "0.3.1", false},
		{"0.10.0", "0.9.9", true},  // string-compare would say false
		{"1.0.0", "0.99.99", true}, // major bump beats anything below
		{"v0.4.0", "0.3.0", true},  // tolerates the leading "v"
		{"0.3.0", "v0.3.0", false},
		{"0.4.0-rc1", "0.3.0", true}, // strips pre-release suffix
		{"0.3.0", "0.4.0-rc1", false},
		{"garbage", "0.3.0", false}, // unparseable → never "newer"
	}
	for _, c := range cases {
		got := isNewerVersion(c.latest, c.current)
		if got != c.want {
			t.Errorf("isNewerVersion(%q, %q) = %v, want %v",
				c.latest, c.current, got, c.want)
		}
	}
}

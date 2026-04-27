// Update-availability nag.
//
// On every command invocation we read a small JSON cache to see if
// GitHub has a newer release than the running binary; if so we print
// a one-line styled notice to stderr after the command runs (similar
// to gh, terraform, kubectl, Amp …). The actual GitHub API call is
// kicked off in a background goroutine when the cache is stale, so
// the user never pays its latency on the hot path — they may just
// see the notice on the *next* invocation after a release lands.
//
// All failure modes (missing cache, no network, rate-limited API)
// are swallowed silently: nagging the user is a quality-of-life
// feature, never a reason for a `bb` invocation to fail or stall.
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// updateCheckInterval is how long we trust a cached "latest version"
// before re-asking GitHub. 24h matches the cadence of every other
// CLI nag I've seen and keeps API hits well under the unauthenticated
// rate limit even for users running `bb` thousands of times a day.
const updateCheckInterval = 24 * time.Hour

// updateCache is the on-disk JSON we persist between invocations to
// avoid hammering the GitHub releases API on every command.
type updateCache struct {
	LastChecked   time.Time `json:"last_checked"`
	LatestVersion string    `json:"latest_version"`
}

// updateCachePath returns the path where we persist the last check
// timestamp + latest known version. Lives under XDG_CACHE_HOME (with
// the standard ~/.cache fallback) so transient state stays out of
// the user's config file.
func updateCachePath() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "bb", "update.json")
}

func readUpdateCache() updateCache {
	p := updateCachePath()
	if p == "" {
		return updateCache{}
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return updateCache{}
	}
	var c updateCache
	_ = json.Unmarshal(data, &c)
	return c
}

func writeUpdateCache(c updateCache) {
	p := updateCachePath()
	if p == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	data, _ := json.Marshal(c)
	_ = os.WriteFile(p, data, 0o644)
}

// notifyIfUpdateAvailable prints a one-line "an update is available"
// banner to stderr when a newer release than the running binary has
// been seen on GitHub. The check is two-phase:
//
//  1. Read the cached latest version (~µs). If it's newer than the
//     running binary, print the banner.
//  2. If the cache is older than updateCheckInterval (or missing),
//     kick off a background goroutine that hits the GitHub releases
//     API and rewrites the cache. Fire-and-forget — if the process
//     exits before the goroutine completes, the next invocation
//     just retries.
//
// We deliberately skip the notice when:
//   - the user has set BB_DISABLE_UPDATE_CHECK=1 (escape hatch for
//     air-gapped / paranoid environments and noisy CI logs);
//   - the binary was built without a real version stamp (`dev` /
//     empty — usually a `go run` or local `go build` where any
//     version compare would be misleading);
//   - the user is currently running `bb upgrade` itself (it already
//     prints version info — a second nag would be redundant);
//   - stderr isn't a terminal (scripts and pipes shouldn't see the
//     banner mixed into structured output).
func notifyIfUpdateAvailable(info BuildInfo) {
	if !shouldCheckForUpdates(info) {
		return
	}

	cache := readUpdateCache()
	if cache.LatestVersion != "" && isNewerVersion(cache.LatestVersion, info.Version) {
		printUpdateNotice(info.Version, cache.LatestVersion)
	}

	if time.Since(cache.LastChecked) > updateCheckInterval {
		go refreshUpdateCache()
	}
}

func shouldCheckForUpdates(info BuildInfo) bool {
	if os.Getenv("BB_DISABLE_UPDATE_CHECK") != "" {
		return false
	}
	if info.Version == "" || info.Version == "dev" {
		return false
	}
	if !term.IsTerminal(int(os.Stderr.Fd())) {
		return false
	}
	// Don't nag during `bb upgrade` — the upgrade command already
	// prints "current / latest" and would just double-print the
	// banner immediately above its own output.
	if len(os.Args) > 1 && os.Args[1] == "upgrade" {
		return false
	}
	return true
}

// refreshUpdateCache is the background half of the update notifier:
// hit GitHub for the latest tag and rewrite the local cache. Always
// updates LastChecked even on a network failure so we don't retry
// the API every command when offline.
func refreshUpdateCache() {
	defer func() {
		// A goroutine panic in a CLI process would dump a confusing
		// stack trace half-way through the user's command output;
		// swallow anything unexpected here.
		_ = recover()
	}()
	c := updateCache{LastChecked: time.Now()}
	rel, err := latestRelease(upgradeOpts{})
	if err == nil && rel != nil {
		c.LatestVersion = strings.TrimPrefix(rel.TagName, "v")
	}
	writeUpdateCache(c)
}

// isNewerVersion returns true when latest is strictly higher than
// current using a tiny semver-ish comparison (major.minor.patch).
// Pre-release suffixes (e.g. "0.4.0-rc1") are tolerated by stripping
// anything after the first '-' / '+'.
func isNewerVersion(latest, current string) bool {
	la := parseVersion(latest)
	cu := parseVersion(current)
	if la == nil || cu == nil {
		return false
	}
	for i := 0; i < 3; i++ {
		if la[i] > cu[i] {
			return true
		}
		if la[i] < cu[i] {
			return false
		}
	}
	return false
}

// parseVersion returns a 3-int slice {major, minor, patch} or nil
// when the input doesn't parse as a basic semver triple.
func parseVersion(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	for _, sep := range []string{"-", "+"} {
		if i := strings.Index(v, sep); i >= 0 {
			v = v[:i]
		}
	}
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return nil
	}
	out := []int{0, 0, 0}
	for i := 0; i < len(parts) && i < 3; i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return nil
		}
		out[i] = n
	}
	return out
}

// printUpdateNotice writes the styled "new release available" banner
// to stderr. Two lines: a coloured headline, then a hint about how
// to upgrade (with the right command for the detected install
// source so brew users see brew, manual users see `bb upgrade`).
func printUpdateNotice(current, latest string) {
	headline := lipgloss.NewStyle().
		Foreground(lipgloss.Color("214")). // amber
		Bold(true).
		Render(fmt.Sprintf("✨ A new bb release is available: %s → %s", current, latest))

	hintCmd := "bb upgrade"
	if pm := detectPackageManager(executablePath()); pm != nil {
		hintCmd = pm.cmd
	}
	hint := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245")).
		Render("Run `" + hintCmd + "` to install (or set BB_DISABLE_UPDATE_CHECK=1 to silence).")

	fmt.Fprintln(os.Stderr, headline)
	fmt.Fprintln(os.Stderr, hint)
}

// executablePath wraps os.Executable so the caller doesn't have to
// handle the error path (we're best-effort here — an unresolved
// path just means we fall back to the generic "bb upgrade" hint).
func executablePath() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	return p
}

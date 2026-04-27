package cmd

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/minio/selfupdate"
	"github.com/spf13/cobra"
)

// upgradeOpts captures network knobs the user can override on the
// command line. Plumbed through latestRelease / downloadFile so both
// hops (the GitHub API call AND the asset download) share the same
// HTTP transport.
type upgradeOpts struct {
	insecure bool // skip TLS verification (corp MITM proxies)
	noProxy  bool // bypass HTTP_PROXY / HTTPS_PROXY env vars
}

// httpClient builds an http.Client honouring the upgrade-time flags.
// The default transport is cloned so we don't mutate the global one.
func (o upgradeOpts) httpClient(timeout time.Duration) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if o.noProxy {
		// Explicitly nil out the proxy func so HTTP_PROXY / HTTPS_PROXY
		// env vars are ignored — matches `curl --noproxy '*'`.
		tr.Proxy = nil
	}
	if o.insecure {
		// Disable cert verification for corporate MITM proxies that
		// re-sign GitHub's certs with their own CA.
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &http.Client{Timeout: timeout, Transport: tr}
}

// GitHub repo to query for releases. Kept here (not configurable) so
// users can't be tricked into upgrading from an attacker-controlled
// fork.
const upgradeRepo = "hugs7/bitbucket-cli"

// newUpgradeCmd implements `bb upgrade`: checks the latest GitHub
// release, downloads the matching archive for the current OS/arch,
// extracts the `bb` binary and atomically replaces the running
// executable. On Windows the running .exe is renamed aside and the
// new one moved into place — no manual restart-shell dance required.
//
// This is only useful for users who installed via a direct binary
// download or the curl|sh script. Users who installed via Homebrew /
// Scoop / Winget / apt should use those package managers instead.
func newUpgradeCmd(info BuildInfo) *cobra.Command {
	var (
		check bool
		force bool
		opts  upgradeOpts
	)
	c := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade bb to the latest release",
		Long: `Download and install the latest release of bb from GitHub.

If you installed bb via Homebrew, Scoop, apt or dnf, prefer your
package manager's update command instead:

  brew upgrade bitbucket-cli
  scoop update bb
  sudo apt update && sudo apt upgrade bitbucket-cli
  sudo dnf upgrade bitbucket-cli

Behind a corporate proxy that intercepts TLS, pass --insecure to skip
cert verification. To bypass an HTTP(S)_PROXY env var entirely, pass
--no-proxy (mirrors curl --noproxy '*').`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpgrade(info, check, force, opts)
		},
	}
	c.Flags().BoolVar(&check, "check", false, "only check for a new version, don't install")
	c.Flags().BoolVar(&force, "force", false, "reinstall even if already on the latest version")
	c.Flags().BoolVarP(&opts.insecure, "insecure", "k", false, "skip TLS verification (corp MITM proxies)")
	c.Flags().BoolVar(&opts.noProxy, "no-proxy", false, "ignore HTTP_PROXY / HTTPS_PROXY env vars")
	return c
}

type ghRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
}

func runUpgrade(info BuildInfo, checkOnly, force bool, opts upgradeOpts) error {
	rel, err := latestRelease(opts)
	if err != nil {
		return fmt.Errorf("check latest release: %w", err)
	}
	latest := strings.TrimPrefix(rel.TagName, "v")
	current := strings.TrimPrefix(info.Version, "v")

	fmt.Printf("current: %s\nlatest:  %s\n", current, latest)

	if !force && current == latest {
		fmt.Println("already up to date")
		return nil
	}
	if checkOnly {
		fmt.Println("a newer version is available — run `bb upgrade` to install")
		return nil
	}

	// Refuse to clobber a binary that a package manager owns:
	// self-replacing /opt/homebrew/Cellar/.../bb or a dpkg-tracked
	// file would put the install into a state where the next
	// `brew upgrade` / `apt upgrade` silently reverts the binary,
	// and worse, leaves the package manager's checksum / inventory
	// out of sync. Manual copies (e.g. `cp bb /opt/homebrew/bin/bb`
	// without going through brew) aren't symlinks into Cellar and
	// aren't tracked by dpkg, so they fall through and self-update
	// normally — covering the user's manual-install path.
	exe, _ := os.Executable()
	if pm := detectPackageManager(exe); pm != nil && !force {
		fmt.Printf("\nbb appears to be managed by %s — use the package manager to upgrade:\n",
			pm.name)
		fmt.Printf("  %s\n\n", pm.cmd)
		fmt.Println("(re-run with --force to ignore this check and replace the binary in place)")
		return nil
	}

	asset, err := pickAsset(rel)
	if err != nil {
		return err
	}
	fmt.Printf("downloading %s (%s)…\n", asset.Name, humanSize(asset.Size))

	bin, cleanup, err := downloadAndExtract(asset.BrowserDownloadURL, asset.Name, opts)
	if err != nil {
		return err
	}
	defer cleanup()

	fmt.Println("installing…")
	if err := selfupdate.Apply(bin, selfupdate.Options{}); err != nil {
		if rerr := selfupdate.RollbackError(err); rerr != nil {
			return fmt.Errorf("install failed and rollback failed: %v (original: %w)", rerr, err)
		}
		return fmt.Errorf("install failed: %w", err)
	}
	fmt.Printf("upgraded to %s ✓\n", latest)
	return nil
}

func latestRelease(opts upgradeOpts) (*ghRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", upgradeRepo)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "bb-upgrade")

	client := opts.httpClient(30 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("github api returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	if rel.TagName == "" {
		return nil, fmt.Errorf("no releases found for %s", upgradeRepo)
	}
	return &rel, nil
}

// pickAsset finds the archive matching this binary's OS/arch using
// the same naming convention as .goreleaser.yaml:
//
//	bb_<version>_<os>_<arch>.{tar.gz,zip}
func pickAsset(rel *ghRelease) (*struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}, error) {
	osName := runtime.GOOS
	arch := runtime.GOARCH

	// GoReleaser writes "darwin"/"linux"/"windows" for goos as-is.
	osTag := osName
	wantExt := ".tar.gz"
	if osName == "windows" {
		wantExt = ".zip"
	}

	for i := range rel.Assets {
		a := &rel.Assets[i]
		n := strings.ToLower(a.Name)
		if !strings.HasSuffix(n, wantExt) {
			continue
		}
		if !strings.Contains(n, "_"+osTag+"_") {
			continue
		}
		if !strings.Contains(n, "_"+arch) {
			continue
		}
		return a, nil
	}
	return nil, fmt.Errorf("no release asset for %s/%s in %s", osName, arch, rel.TagName)
}

// downloadAndExtract fetches the archive and returns an open reader
// over the `bb` binary inside it, plus a cleanup func to remove the
// temp dir after Apply() finishes.
func downloadAndExtract(url, name string, opts upgradeOpts) (io.Reader, func(), error) {
	tmp, err := os.MkdirTemp("", "bb-upgrade-*")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }

	archivePath := filepath.Join(tmp, name)
	if err := downloadFile(url, archivePath, opts); err != nil {
		cleanup()
		return nil, nil, err
	}

	binName := "bb"
	if runtime.GOOS == "windows" {
		binName = "bb.exe"
	}
	extracted := filepath.Join(tmp, binName)

	switch {
	case strings.HasSuffix(name, ".zip"):
		if err := extractZip(archivePath, binName, extracted); err != nil {
			cleanup()
			return nil, nil, err
		}
	case strings.HasSuffix(name, ".tar.gz"):
		if err := extractTarGz(archivePath, binName, extracted); err != nil {
			cleanup()
			return nil, nil, err
		}
	default:
		cleanup()
		return nil, nil, fmt.Errorf("unsupported archive: %s", name)
	}

	f, err := os.Open(extracted)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	// Wrap cleanup to also close the file.
	wrapped := func() { _ = f.Close(); cleanup() }
	return f, wrapped, nil
}

func downloadFile(url, dst string, opts upgradeOpts) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "bb-upgrade")
	client := opts.httpClient(5 * time.Minute)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

func extractZip(src, want, dst string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		if filepath.Base(f.Name) != want {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, rc)
		return err
	}
	return fmt.Errorf("%s not found in %s", want, src)
}

func extractTarGz(src, want, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if filepath.Base(hdr.Name) != want {
			continue
		}
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, tr)
		return err
	}
	return fmt.Errorf("%s not found in %s", want, src)
}

// pkgManager is one detected install source — what to call it in
// user-facing messages, plus the command the user should run to
// upgrade through that channel.
type pkgManager struct {
	name string
	cmd  string
}

// detectPackageManager returns a non-empty pkgManager when the given
// executable path looks like it was installed (and is therefore
// owned) by a system package manager. Detection is best-effort and
// deliberately conservative: an unknown / ambiguous path returns the
// zero value so we still self-update for plain manual installs.
//
// We resolve symlinks first so that a /opt/homebrew/bin/bb pointing
// into /opt/homebrew/Cellar/bitbucket-cli/<v>/bin/bb is identified
// as a brew install, while a regular file copied into the same
// directory by hand is not.
func detectPackageManager(execPath string) *pkgManager {
	if execPath == "" {
		return nil
	}
	real, err := filepath.EvalSymlinks(execPath)
	if err != nil {
		real = execPath
	}

	// Homebrew stores every artefact under <prefix>/Cellar/<formula>/
	// and exposes them via symlinks in <prefix>/bin. The Cellar
	// substring is unique to brew so we can match either Apple
	// Silicon (/opt/homebrew/Cellar) or Intel (/usr/local/Cellar /
	// /home/linuxbrew/.linuxbrew/Cellar) installs in one check.
	if strings.Contains(real, "/Cellar/") {
		return &pkgManager{name: "Homebrew", cmd: "brew upgrade bitbucket-cli"}
	}

	// dpkg / rpm only matter on Linux. We avoid spawning these on
	// macOS where they may exist as Homebrew formulae (`dpkg` is on
	// brew) but never own /usr/local files in the system sense.
	if runtime.GOOS == "linux" {
		// dpkg -S <path> exits 0 + prints "<pkg>: <path>" when the
		// file is tracked, non-zero on miss. We discard stderr by
		// using Output() so the "no path found" diagnostic doesn't
		// leak into our own output on a clean miss.
		if _, err := exec.LookPath("dpkg"); err == nil {
			if out, err := exec.Command("dpkg", "-S", real).Output(); err == nil && len(out) > 0 {
				return &pkgManager{name: "apt/dpkg", cmd: "sudo apt update && sudo apt upgrade bitbucket-cli"}
			}
		}
		if _, err := exec.LookPath("rpm"); err == nil {
			if out, err := exec.Command("rpm", "-qf", real).Output(); err == nil &&
				len(out) > 0 && !strings.Contains(string(out), "not owned by any package") {
				return &pkgManager{name: "rpm/dnf", cmd: "sudo dnf upgrade bitbucket-cli"}
			}
		}
	}

	return nil
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

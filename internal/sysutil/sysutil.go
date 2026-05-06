// Package sysutil holds tiny OS-level helpers shared across packages.
// Kept deliberately dependency-free — only the standard library — so
// any package in the tree can pull it in without import cycles.
package sysutil

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// OpenInBrowser launches the platform's default browser pointing at url.
// Returns immediately after spawning the helper process; any errors
// from the browser itself are not surfaced.
func OpenInBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler"}
	default:
		cmd = "xdg-open"
	}
	args = append(args, url)
	return exec.Command(cmd, args...).Start()
}

// CopyToClipboard writes text to the system clipboard. Tries the
// platform-native helper first (pbcopy / clip / clip.exe under WSL)
// and falls back to wl-copy, xclip, or xsel on Linux. Returns an
// error if no helper is available or the helper fails.
func CopyToClipboard(text string) error {
	candidates := clipboardCandidates()
	if len(candidates) == 0 {
		return errNoClipboard
	}
	var lastErr error
	for _, c := range candidates {
		if _, err := exec.LookPath(c.cmd); err != nil {
			lastErr = err
			continue
		}
		cmd := exec.Command(c.cmd, c.args...)
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return errNoClipboard
}

type clipboardCmd struct {
	cmd  string
	args []string
}

func clipboardCandidates() []clipboardCmd {
	switch runtime.GOOS {
	case "darwin":
		return []clipboardCmd{{cmd: "pbcopy"}}
	case "windows":
		return []clipboardCmd{{cmd: "clip"}}
	default:
		// Linux (incl. WSL). Prefer clip.exe under WSL so the
		// copied text lands in the Windows clipboard the user
		// actually pastes from. Fall back to native helpers.
		out := []clipboardCmd{}
		if IsWSL() {
			out = append(out, clipboardCmd{cmd: "clip.exe"})
		}
		out = append(out,
			clipboardCmd{cmd: "wl-copy"},
			clipboardCmd{cmd: "xclip", args: []string{"-selection", "clipboard"}},
			clipboardCmd{cmd: "xsel", args: []string{"--clipboard", "--input"}},
		)
		return out
	}
}

var errNoClipboard = &clipboardErr{msg: "no clipboard helper available"}

type clipboardErr struct{ msg string }

func (e *clipboardErr) Error() string { return e.msg }

// IsWSL reports whether bb is running inside the Windows Subsystem for
// Linux. WSL exposes itself via the WSL_DISTRO_NAME / WSL_INTEROP env
// vars and the string "microsoft" in /proc/version. The result is
// cached because callers (e.g. the PTY editor branch) hit it on every
// keystroke and the underlying file read is wasteful otherwise.
func IsWSL() bool {
	wslOnce.Do(func() {
		if runtime.GOOS != "linux" {
			return
		}
		if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
			wslCached = true
			return
		}
		// /proc/version on WSL contains "Microsoft" or "microsoft"
		// (capitalised differently across WSL1 / WSL2 / kernel
		// builds). Read failure → assume not WSL.
		if data, err := os.ReadFile("/proc/version"); err == nil {
			if strings.Contains(strings.ToLower(string(data)), "microsoft") {
				wslCached = true
			}
		}
	})
	return wslCached
}

var (
	wslOnce   sync.Once
	wslCached bool
)

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

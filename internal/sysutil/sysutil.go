// Package sysutil holds tiny OS-level helpers shared across packages.
// Kept deliberately dependency-free — only the standard library — so
// any package in the tree can pull it in without import cycles.
package sysutil

import (
	"os/exec"
	"runtime"
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

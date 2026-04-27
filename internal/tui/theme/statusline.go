// Package theme — footer / status-line composition.
//
// Every TUI renders its footer the same way: an optional toast (the
// "loading…" spinner or a ✓/✗ status message) prefixed to the
// help-bar. Centralised here so home, repo, and pr can stay aligned.
package theme

import "strings"

// RenderStatusLine produces the toast string shown in the footer of
// every TUI: a coloured "loading…" with a spinner glyph, a "✓"/"✗"
// (or 3270 "OK"/"X") prefixed user toast, or empty when nothing is in
// flight.
func RenderStatusLine(loading bool, spinnerView, status string) string {
	switch {
	case loading:
		if Mainframe() {
			return StatusInfo.Render("X SYSTEM " + spinnerView)
		}
		return StatusInfo.Render(spinnerView + " loading…")
	case status == "":
		return ""
	case strings.HasPrefix(status, "✗"), strings.HasPrefix(status, "X "):
		return StatusErr.Render(status)
	case strings.HasPrefix(status, "✓"), strings.HasPrefix(status, "OK "):
		return StatusOK.Render(status)
	default:
		return StatusInfo.Render(status)
	}
}

// JoinFooter prepends the status line (when non-empty) to the help
// view, separated by the dim bullet used elsewhere in title bars.
// Returns just the help line when status is empty so the footer
// never grows vertically.
func JoinFooter(status, help string) string {
	if status == "" {
		return help
	}
	return status + "  " + TitleSep + "  " + help
}
